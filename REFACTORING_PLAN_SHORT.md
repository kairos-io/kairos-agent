# Partition Encryption Refactoring - Summary

## ✅ REFACTORING STATUS (Updated: 2025-10-24)

### Completed:
- ✅ **kairos-sdk/kcrypt/encryptor.go** (NEW): Interface-based encryption system
  - `PartitionEncryptor` interface with `Encrypt()`, `Unlock()`, `Name()`, `Validate()` methods
  - Three implementations: `RemoteKMSEncryptor`, `TPMWithPCREncryptor`, `LocalTPMNVEncryptor`
  - `GetEncryptor()` factory with decision logic and validation
  - Config scanning done once in `GetEncryptor()`, stored in encryptor instances
  
- ✅ **kairos-sdk/kcrypt/tpm_passphrase.go** (NEW): Local TPM NV passphrase management
  - `GetOrCreateLocalTPMPassphrase()` - Moved from kcrypt-challenger
  - `generateAndStoreLocalTPMPassphrase()` - Helper function
  
- ✅ **kairos-sdk/kcrypt/lock.go**: New encryption functions
  - `EncryptWithLocalTPMPassphrase()` - Encrypts without plugin
  - `luksifyWithPassphrase()` - Low-level encryption with explicit passphrase
  
- ✅ **kairos-agent/internal/agent/hooks/encrypt.go** (NEW - 564 lines): All encryption logic
  - Unified `Encrypt()` method for UKI and non-UKI modes
  - Uses `PartitionEncryptor` interface for clean separation
  - Helper methods: `determinePartitionsToEncrypt()`, `preparePartitionsForEncryption()`, 
    `backupOEMIfNeeded()`, `restoreOEMIfNeeded()`, `copyCloudConfigToOEM()`, `udevAdmSettle()`
  - Legacy methods: `EncryptNonUKI()`, `EncryptUKI()` (kept for backward compatibility)
  - OEM backup happens BEFORE unmounting (more efficient)
  - Cloud-config copied BEFORE encryption (preserved in OEM backup)
  - udevadm settle now settles actual partition devices (not hardcoded)
  - Removed custom `containsString()`, using `slices.Contains()`
  - Simplified function signatures (removed redundant parameters)
  
- ✅ **kairos-agent/internal/agent/hooks/finish.go** (SIMPLIFIED - 51 lines): Clean orchestration
  - Only contains `Finish` hook and its `Run()` method
  - Minimal imports (config and v1 types only)
  - Calls `Encrypt()` directly without redundant checks
  
- ✅ **kairos-agent/go.mod**: Added `replace` directive for local kairos-sdk development

### Decision Logic (Implemented):
1. If `challenger_server` or `mdns` configured → **Remote KMS** (both UKI & non-UKI)
2. Else if UKI mode → **TPM + PCR policy** (validates systemd ≥ 252, TPM 2.0)
3. Else (non-UKI, no remote) → **Local TPM NV passphrase**

### Recent Improvements (2025-10-24):
- **File Organization**: Moved all encryption logic to dedicated `encrypt.go` (564 lines)
  - `finish.go` simplified from ~600 lines to 51 lines
  - Better separation of concerns and maintainability
- **Cloud-Config**: Extracted to function, happens before encryption
- **udevadm Settle**: Moved inside Encrypt(), settles actual partition devices
- **Simplified Flow**: Removed redundant condition checks

### Pending:
- ⏳ **kcrypt-challenger**: Remove local TPM NV logic (now in kairos-sdk)
  - Files to update: `cmd/discovery/client/client.go`, `cmd/discovery/client/enc.go`
  - Remove `localPass()` function and local passphrase fallback logic
- ⏳ **Testing**: End-to-end testing of all three encryption methods
- ⏳ **immucore**: Consider updating to use new `PartitionEncryptor` interface (optional)

### Architecture Benefits Achieved:
✅ Single Responsibility: Each encryptor handles both encryption AND decryption
✅ No Config Duplication: Config scanned once, stored in encryptor
✅ Clean Interface: Easy to add new encryption methods
✅ Reusable: Other projects (immucore) can use the same interface
✅ Testable: Each encryptor can be tested independently
✅ Maintainable: Clear separation of concerns

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
