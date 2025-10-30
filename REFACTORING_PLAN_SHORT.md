# Partition Encryption Refactoring - Summary

## ✅ REFACTORING STATUS (Updated: 2025-10-24)

### Implementation Complete - Interface-Based Approach ✅

We implemented a cleaner architecture using the **Strategy Pattern** with a `PartitionEncryptor` interface.

#### Completed Work:

**1. kairos-sdk/kcrypt/encryptor.go (NEW FILE)**
- `PartitionEncryptor` interface with `Encrypt()`, `Unlock()`, `Name()`, `Validate()` methods
- Three implementations: `RemoteKMSEncryptor`, `TPMWithPCREncryptor`, `LocalTPMNVEncryptor`
- `GetEncryptor(logger)` factory with automatic config scanning and validation

**2. kairos-sdk/kcrypt/tpm_passphrase.go (NEW FILE)**
- Local TPM NV passphrase management (moved from kcrypt-challenger)

**3. kairos-sdk/kcrypt/lock.go + unlock.go (MODIFIED)**
- New encryption functions for local TPM NV passphrase
- **SECURITY FIX** ✅: Removed passphrase logging (only log length)

**4. kairos-agent/internal/agent/hooks/encrypt.go (NEW - ~350 LINES)**
- Unified `Encrypt()` method for both UKI and non-UKI modes
- Helper methods: `determinePartitionsToEncrypt()`, `preparePartitionsForEncryption()`,
  `backupOEMIfNeeded()`, `restoreOEMIfNeeded()`, `copyCloudConfigToOEM()`, `udevAdmSettle()`
- **BUG FIX** ✅: `GetEncryptor` called BEFORE unmounting OEM (so it can read kcrypt config)
- **BUG FIX** ✅: `restoreOEMIfNeeded` uses dmsetup to find mapper device + udev settle
- Legacy methods: `EncryptNonUKI()`, `EncryptUKI()` (backward compatibility)

**5. kairos-agent/internal/agent/hooks/finish.go (SIMPLIFIED - 51 LINES)**
- Clean orchestration, minimal imports

**6. kairos-agent/internal/agent/hooks/hook.go (MODIFIED)**
- **BUG FIX** ✅: `lockPartitions()` uses `dmsetup ls` to properly close mapper devices

#### Critical Bug Fixes (2025-10-24):

**Issue 1: Challenger Config Ignored** ✅
- Root cause: `GetEncryptor` called after OEM unmounted
- Fix: Call `GetEncryptor` at step 1.5 (before unmounting)
- Result: Challenger server now properly detected and used

**Issue 2: OEM Restore Failed** ✅
- Root cause: `blkid -L` returned LUKS container, not mapper device; device node not ready
- Fix: Use `dmsetup ls` to verify mapper exists + `udevAdmSettle()` to wait for device node
- Result: OEM successfully restored after encryption

**Issue 3: Passphrase Logging** ✅
- Root cause: Passphrases logged in plaintext
- Fix: Removed from all log messages, only log length
- Result: No sensitive data in logs

**Issue 4: Partition Locking Failed** ✅
- Root cause: Tried to close by label path instead of mapper name
- Fix: Use `dmsetup ls --target crypt` to find active mappers
- Result: All encrypted devices properly closed

#### Decision Logic:
1. If `challenger_server` or `mdns` → **Remote KMS** (both UKI & non-UKI)
2. Else if UKI mode → **TPM + PCR policy**
3. Else → **Local TPM NV passphrase**

#### Production Status:
- ✅ All code compiles successfully
- ✅ Challenger-based encryption tested and working
- ✅ OEM backup/restore working
- ✅ Encrypted devices properly locked
- ✅ No security issues
- ⚠️ Remove `replace` directive from go.mod before production merge

### Remaining Work:
- ⏳ **kcrypt-challenger cleanup**: Remove local TPM NV logic (now in kairos-sdk)
- ⏳ **Testing**: Full end-to-end testing of all three encryption methods
- ⏳ **Cloud-init path bug**: Fix mkdir on file paths in `pkg/utils/runstage.go:83`
- ⏳ **immucore integration** (optional)

---

## Proposed Architecture (ORIGINAL PLAN)

### Component Responsibilities

- **kairos-agent**: Orchestrates encryption with unified code path for UKI and non-UKI modes. Decides passphrase source based on config (remote KMS configured → use plugin, otherwise local). Handles common operations: unmount partitions, backup/restore OEM, unlock/wait for encrypted devices.

- **kairos-sdk**: Provides single `Encrypt()` function that accepts passphrase source parameter (remote/local-TPM/ephemeral). Handles all LUKS creation, PCR policy enrollment, and partition formatting. Contains local TPM NV passphrase storage/retrieval logic.

- **kcrypt-challenger**: Focused solely on remote KMS operations - implements TPM attestation protocol to retrieve passphrases from remote server. No local encryption logic.

## Unified Encryption Flow (kairos-agent)

```go
func encryptPartitions(config Config, isUKI bool) error {
    // 1. Common preparation (extracted methods)
    partitions := determinePartitionsToEncrypt(config, isUKI)
    preparePartitionsForEncryption(partitions)  // unmount all
    oemBackup := backupOEMIfNeeded(partitions)  // backup OEM data
    defer restoreOEMIfNeeded(oemBackup)

    // 2. For each partition
    for _, partition := range partitions {
        // 2a. Determine passphrase source
        source := determinePassphraseSource(config, isUKI)
        // Options: "remote" (KMS), "local_tpm" (NV memory), "ephemeral" (random)

        // 2b. Get passphrase when needed
        passphrase := getPassphrase(partition, config, source)

        // 2c. Encrypt with unified logic (kairos-sdk)
        kcrypt.Encrypt(
            partition,
            passphrase,
            pcrBinding: isUKI ? config.BindPCRs : nil,
            keepPasswordSlot: source != "ephemeral",
        )
        // This decides:
        // - Creates LUKS with passphrase
        // - If pcrBinding: adds TPM PCR policy keyslot
        // - If !keepPasswordSlot: wipes password keyslot (TPM-only unlock)
    }

    // 3. Common cleanup (extracted methods)
    unlockEncryptedPartitions(partitions)
    waitForUnlockedPartitions(partitions)
    lockPartitions(partitions)

    return nil
}
```

**Key Points:**
- Passphrase retrieval is a separate step, only called when needed
- Encryption logic (kairos-sdk) decides whether to keep password keyslot based on source
- Common operations extracted into reusable methods
- Same flow for UKI and non-UKI, only parameters differ

## Key Changes by Repository

- **kairos-agent**: Merge `Encrypt()` and `EncryptUKI()` into unified flow, extract common operations (unmount, backup OEM, unlock, restore), simple decision logic for passphrase source
- **kairos-sdk**: Add unified `Encrypt()` function with passphrase source parameter, move local TPM NV logic from kcrypt-challenger, consolidate all LUKS/PCR operations
- **kcrypt-challenger**: Remove local TPM NV logic, remain as remote KMS client only (no changes to remote attestation protocol)

## Benefits

✅ UKI gains remote KMS support
✅ Single code path eliminates duplication
✅ Clear separation: remote=plugin, local=sdk
✅ Backwards compatible
