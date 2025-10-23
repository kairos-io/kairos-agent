# Partition Encryption Refactoring Plan

## ✅ REFACTORING STATUS (Updated: 2025-10-22)

### Implementation Complete - Interface-Based Approach

We implemented a cleaner architecture using the **Strategy Pattern** with a `PartitionEncryptor` interface:

#### Completed Work:

**1. kairos-sdk/kcrypt/encryptor.go (NEW FILE)**
- `PartitionEncryptor` interface with methods:
  - `Encrypt(partition string) error` - Encrypts a partition
  - `Unlock() error` - Unlocks encrypted partitions (handles decryption)
  - `Name() string` - Returns encryption method name for logging
  - `Validate() error` - Validates prerequisites (systemd version, TPM device, etc.)

- Three concrete implementations:
  - `RemoteKMSEncryptor`: Uses kcrypt-challenger plugin for remote KMS
  - `TPMWithPCREncryptor`: Uses systemd-cryptenroll with PCR policy (UKI mode)
  - `LocalTPMNVEncryptor`: Uses local TPM NV memory for passphrase storage

- `GetEncryptor(cfg EncryptorConfig) (PartitionEncryptor, error)`: Factory function
  - Scans kcrypt config ONCE and stores it in encryptor
  - Implements decision logic (remote KMS → TPM+PCR → local TPM NV)
  - Validates prerequisites before returning encryptor

- Helper functions moved from kairos-agent:
  - `validateSystemdVersion()` - Checks systemd >= required version
  - `validateTPMDevice()` - Checks TPM 2.0 device exists

**2. kairos-sdk/kcrypt/tpm_passphrase.go (NEW FILE)**
- `GetOrCreateLocalTPMPassphrase()` - Retrieves or generates TPM NV passphrase
- `generateAndStoreLocalTPMPassphrase()` - Stores passphrase in TPM NV memory
- Logic moved from kcrypt-challenger to centralize local TPM operations

**3. kairos-sdk/kcrypt/lock.go (MODIFIED)**
- `EncryptWithLocalTPMPassphrase()` - Encrypts using local TPM NV passphrase
- `luksifyWithPassphrase()` - Low-level LUKS encryption with explicit passphrase

**4. kairos-agent/internal/agent/hooks/finish.go (MAJOR REFACTOR)**
- Unified `Encrypt()` method replaces separate UKI/non-UKI paths
- Uses `kcrypt.GetEncryptor()` to get appropriate encryptor
- Calls `encryptor.Encrypt()` for each partition
- Calls `encryptor.Unlock()` for decryption (no more manual type detection)

- Extracted helper methods from old code:
  - `determinePartitionsToEncrypt()` - Respects user config, defaults by mode
  - `preparePartitionsForEncryption()` - Finds devices, unmounts partitions
  - `backupOEMIfNeeded()` - Backs up OEM BEFORE unmounting (improved timing)
  - `restoreOEMIfNeeded()` - Restores OEM after encryption
  - `unlockEncryptedPartitions()` - Uses encryptor's Unlock() method
  - `waitForUnlockedPartitions()` - Waits for devices with retry logic

- Code quality improvements:
  - Removed custom `containsString()`, using `slices.Contains()`
  - Removed redundant function parameters
  - Config scanning centralized in `GetEncryptor()`

**5. kairos-agent/go.mod (MODIFIED)**
- Added `replace github.com/kairos-io/kairos-sdk => ../kairos-sdk` for local development

#### Decision Logic (Implemented in GetEncryptor):
```
1. If kcrypt config has challenger_server OR mdns configured
   → RemoteKMSEncryptor (both UKI & non-UKI)
   
2. Else if UKI mode
   → TPMWithPCREncryptor (validates systemd ≥ 252, TPM 2.0)
   
3. Else (non-UKI, no remote KMS)
   → LocalTPMNVEncryptor (validates TPM 2.0)
```

#### Architecture Benefits Achieved:
✅ **Single Responsibility**: Each encryptor handles both encryption AND decryption
✅ **No Config Duplication**: Config scanned once in GetEncryptor, stored in encryptor
✅ **Clean Interface**: Easy to add new encryption methods (just implement interface)
✅ **Reusable**: Other projects (immucore) can use the same interface
✅ **Testable**: Each encryptor can be unit tested independently
✅ **Maintainable**: Clear separation of concerns, no type detection in agent code

#### Files Changed:
- ✅ `kairos-sdk/kcrypt/encryptor.go` (NEW)
- ✅ `kairos-sdk/kcrypt/tpm_passphrase.go` (NEW)
- ✅ `kairos-sdk/kcrypt/lock.go` (MODIFIED)
- ✅ `kairos-agent/internal/agent/hooks/finish.go` (MAJOR REFACTOR)
- ✅ `kairos-agent/go.mod` (MODIFIED)

#### Remaining Work:
- ⏳ **kcrypt-challenger cleanup**: Remove local TPM NV logic
  - Files: `cmd/discovery/client/client.go`, `cmd/discovery/client/enc.go`
  - Remove `localPass()` function and local passphrase fallback
  - Keep only remote KMS attestation logic
  
- ⏳ **Testing**: End-to-end testing of all three encryption methods
  - Remote KMS encryption + unlock
  - TPM+PCR encryption + unlock (UKI)
  - Local TPM NV encryption + unlock (non-UKI)
  
- ⏳ **immucore integration** (OPTIONAL): Update to use new PartitionEncryptor interface

#### Notes for Next Session:
- All code compiles successfully
- The interface-based approach is cleaner than the original plan's function-based approach
- Config scanning happens once in GetEncryptor, avoiding redundant filesystem/cmdline scans
- Each encryptor knows how to unlock itself (no need for caller to detect encryption type)
- Remove `replace` directive from go.mod before merging to production

---

## Executive Summary (ORIGINAL PLAN)

### Problem
UKI and non-UKI installation paths have completely separate encryption code, even though they perform similar operations. The main issues are:
1. **Code duplication** between `Encrypt()` and `EncryptUKI()`
2. **UKI mode doesn't support remote KMS** (kcrypt-challenger) despite no technical limitation
3. **Different encryption methods** without a clear architecture

### Investigation Findings
Analysis of the codebase reveals that:
- **Non-UKI** always delegates to the `kcrypt-challenger` plugin (even for local encryption)
- **UKI** directly calls `kairos-sdk` functions and uses `systemd-cryptenroll`
- There are **THREE different encryption methods** in use:
  1. Remote KMS (via kcrypt-challenger server)
  2. Local TPM NV memory (via kcrypt-challenger plugin)
  3. Local PCR-bound (via systemd-cryptenroll, UKI only)

### Solution
**Simplify responsibilities: Remote vs Local**

1. **kcrypt-challenger plugin** → Only for remote KMS (server-based attestation)
   - Implements TPM attestation protocol with remote server
   - Returns passphrase from remote KMS
   
2. **kairos-sdk** → All local encryption methods
   - Local with PCR policy (systemd-cryptenroll) - current UKI
   - Local with TPM NV storage (tpm-helpers) - current non-UKI local
   - Remote KMS passphrase + optional PCR policy (NEW hybrid)

3. **kairos-agent** → Simple decision logic
   - If `kcrypt.challenger.challenger_server` configured → use kcrypt-challenger plugin
   - Else → use kairos-sdk local encryption
   - In UKI mode: optionally add PCR binding regardless of passphrase source

### Benefits
✅ Clear separation of concerns (remote = plugin, local = sdk)
✅ UKI gains remote KMS capability
✅ No plugin changes needed for local encryption
✅ Simpler decision logic in kairos-agent
✅ Code consolidation in kairos-agent
✅ Backwards compatible

### Implementation Effort
- **Phase 0** (Move local TPM to kairos-sdk): ~6 hours
- **Phase 1** (Add hybrid functions): ~8 hours
- **Phase 2** (Extract common operations in kairos-agent): ~8 hours
- **Phase 3** (Integration & decision logic): ~6 hours
- **Phase 4** (Testing/Docs): ~8 hours
- **Total:** ~36 hours (~4.5 days)

---

## 🔍 Technical Investigation: Encryption Flow Analysis

### Overview

The non-UKI and UKI cases use fundamentally different encryption implementations:

1. **Non-UKI Path**: Always delegates to `kcrypt-challenger` plugin (even for local encryption)
2. **UKI Path**: Directly calls `kairos-sdk/kcrypt` functions (no plugin involved)

### Detailed Analysis

#### Non-UKI Local Encryption Flow:
```
ENCRYPTION:
kairos-agent
  └─> kcrypt.EncryptWithConfig(partition, logger, nil)  [kairos-sdk]
       └─> getPassword() via plugin bus
            └─> kcrypt-challenger plugin
                 └─> localPass()
                      └─> Read from TPM NV index OR generate new random passphrase
                      └─> Store passphrase in TPM NV memory (encrypted with TPM)
                      └─> Return passphrase
       └─> createLuks(device, passphrase)
            └─> LUKS created with passphrase in keyslot

UNLOCK:
kairos-agent
  └─> kcrypt.UnlockAll(tpm=false, ...)  [kairos-sdk]
       └─> getPassword() via plugin bus
            └─> kcrypt-challenger plugin
                 └─> localPass()
                      └─> Read passphrase from TPM NV memory
                      └─> Decrypt with TPM
                      └─> Return passphrase
       └─> cryptsetup luksOpen with passphrase

KEY POINT: Passphrase is persistent in both TPM NV memory AND LUKS keyslot
```

**Key Code Locations:**
- `kairos-sdk/kcrypt/lock.go::luksifyWithConfig()` - Calls `getPassword()`
- `kairos-sdk/kcrypt/unlock.go::getPassword()` - Uses plugin bus
- `kcrypt-challenger/cmd/discovery/client/client.go::GetPassphrase()` - Plugin entry point
- `kcrypt-challenger/cmd/discovery/client/enc.go::localPass()` - TPM NV memory operations

#### UKI Local Encryption Flow:
```
ENCRYPTION:
kairos-agent
  └─> kcrypt.EncryptWithPcrs(partition, publicPCRs, pcrs, logger)  [kairos-sdk]
       └─> luksifyMeasurements()
            └─> pass = randomString(32)  [ephemeral, never stored]
            └─> createLuks(device, pass)  [LUKS created with password keyslot]
            └─> systemd-cryptenroll --tpm2-public-key=... --tpm2-pcrs=... device
                 • Reads current PCR values from TPM
                 • Creates TPM2 policy keyslot in LUKS
                 • Policy: "unlock if PCRs match these values"
            └─> systemd-cryptenroll --wipe-slot=password device
                 • Removes password keyslot
                 • Only TPM2 policy keyslot remains

UNLOCK:
kairos-agent
  └─> kcrypt.UnlockAll(tpm=true, ...)  [kairos-sdk]
       └─> systemd-cryptsetup attach <device> <name> - tpm2-device=auto
            └─> systemd reads TPM2 policy from LUKS keyslot
            └─> TPM checks: do current PCRs match policy?
            └─> If yes: TPM unlocks LUKS directly (no passphrase involved)
            └─> If no: unlock fails

KEY POINT: No passphrase is stored anywhere. TPM unlocks using hardware-based policy.
```

**Key Code Locations:**
- `kairos-sdk/kcrypt/lock.go::luksifyMeasurements()` - Direct implementation
- `kairos-sdk/kcrypt/unlock.go:62` - Uses `systemd-cryptsetup` command-line tool

### Key Differences:

| Aspect | Non-UKI Local | UKI Local |
|--------|---------------|-----------|
| **Implementation** | Plugin-based (kcrypt-challenger) | Direct (kairos-sdk) |
| **Passphrase Storage** | TPM NV memory (encrypted blob) | No passphrase stored |
| **LUKS Keyslot** | ✅ Password keyslot with passphrase | ❌ Only TPM2 policy keyslot |
| **TPM Role** | Storage for passphrase | Direct unlock via PCR policy |
| **PCR Binding** | ❌ No PCR binding | ✅ PCR binding (default: 11) |
| **Unlock Method** | 1. Read passphrase from TPM NV<br>2. `cryptsetup luksOpen` with passphrase | `systemd-cryptsetup` with `tpm2-device=auto`<br>(TPM unlocks directly) |
| **Recovery** | ✅ Can manually unlock if passphrase extracted | ❌ No manual unlock possible |
| **Dependencies** | tpm-helpers library | systemd >= 252 |
| **Configuration** | `kcrypt.challenger.nv_index` | `bind-pcrs`, `bind-public-pcrs` |

### Remote KMS (Attestation) Flow:
```
Both modes SHOULD support this, but currently only non-UKI does:

kairos-agent
  └─> kcrypt.EncryptWithConfig(partition, logger, kcryptConfig)
       └─> getPassword() via plugin bus
            └─> kcrypt-discovery-challenger plugin
                 └─> GetPassphrase()
                      └─> waitPassWithTPMAttestation()
                           └─> WebSocket connection to challenger server
                           └─> TPM attestation flow (EK, AK, PCR quote)
                           └─> Receives passphrase from remote KMS
```

### Decision Points:

#### Option A: Keep Plugin Architecture (RECOMMENDED)
**Pros:**
- Maintains separation of concerns
- kcrypt-challenger plugin already handles both local and remote KMS
- Can swap encryption backends without changing kairos-agent
- Plugin can be updated independently

**Cons:**
- Adds indirection for UKI case
- Two different local encryption methods (TPM NV vs systemd policy)

#### Option B: Move Everything to kairos-sdk
**Pros:**
- Single code path, no plugin overhead
- Direct function calls

**Cons:**
- Loses plugin flexibility
- kcrypt-challenger becomes less useful
- Harder to extend with new encryption methods

#### Option C: Hybrid Approach
**Pros:**
- Direct calls for simple cases
- Plugin for complex cases (remote KMS)

**Cons:**
- Still have multiple code paths
- Complexity in deciding which path to use

### Key Understanding: kcrypt-challenger's Role

**kcrypt-challenger is a passphrase provider, NOT an encryption engine.**

The actual LUKS encryption is always performed by `kairos-sdk/kcrypt` using `cryptsetup`. The plugin's job is to provide passphrases through different methods:
- Remote KMS via TPM attestation
- Local TPM NV memory storage
- (In theory) any other passphrase source

### Current Architecture:

**The architectural difference is:**

1. **UKI mode doesn't support remote KMS because:**
   - Current `EncryptWithPcrs()` generates its own ephemeral password internally
   - It never asks kcrypt-challenger for a passphrase
   - Password is wiped after TPM enrollment

2. **Two distinct unlock models:**
   
   **Model A: Passphrase-based (current non-UKI)**
   - Plugin provides passphrase (from remote KMS OR local TPM-NV storage)
   - kairos-sdk encrypts with that passphrase
   - Passphrase persists in LUKS keyslot
   - **Unlock:** Retrieve same passphrase again, feed to `cryptsetup luksOpen`
   - Can unlock via passphrase OR via plugin
   
   **Model B: TPM Policy-based (current UKI)**
   - **Encryption:** Ephemeral random password used to create LUKS
   - `systemd-cryptenroll` stores TPM2 policy in LUKS keyslot (not a password!)
   - Password keyslot is wiped - password never stored anywhere
   - **Unlock:** `systemd-cryptsetup attach <device> - tpm2-device=auto`
     - No passphrase needed
     - TPM hardware unlocks directly based on PCR measurements
     - If current PCRs match enrolled policy → unlock succeeds
     - If PCRs don't match → unlock fails
   - More secure (no passphrase to extract, hardware-bound)

3. **For UKI + Remote KMS, we need a hybrid approach:**
   - Get passphrase from remote KMS (via kcrypt-challenger)
   - Use that passphrase for LUKS encryption
   - **ALSO** enroll TPM2 policy with PCR binding
   - **Decision needed:** Keep password keyslot OR wipe it?
     - **Option 1:** Keep both keyslots
       - Can unlock via passphrase (retrieved from KMS) OR via TPM
       - Provides fallback if TPM fails or PCRs change unexpectedly
     - **Option 2:** Wipe password keyslot (TPM-only)
       - Only TPM can unlock (more secure)
       - Must contact KMS server every time to get passphrase during boot
       - But passphrase is only used to re-enroll if PCRs change

### Implementation Plan:

#### Phase 0: Consolidate Passphrase Acquisition Logic

**Key Realization:**

All encryption flows need a passphrase at some point. The rest of the encryption logic (creating LUKS, formatting, PCR enrollment) is the same. We should extract passphrase acquisition into a single method.

**Changes to kairos-sdk/kcrypt:**

1. **Create unified passphrase acquisition function:**
   
   **Location:** `kairos-sdk/kcrypt/passphrase.go` (new file)
   
   ```go
   type PassphraseSource string
   
   const (
       PassphraseSourceRemote    PassphraseSource = "remote"     // From KMS via plugin
       PassphraseSourceLocalTPM  PassphraseSource = "local_tpm"  // From TPM NV memory
       PassphraseSourceEphemeral PassphraseSource = "ephemeral"  // Generated, to be discarded
   )
   
   // GetPassphrase returns a passphrase for encryption based on config
   func GetPassphrase(
       partition *block.Partition,
       kcryptConfig *bus.DiscoveryPasswordPayload,
       source PassphraseSource,
       logger types.KairosLogger,
   ) (passphrase string, shouldKeepKeyslot bool, err error) {
       switch source {
       case PassphraseSourceRemote:
           // Call kcrypt-challenger plugin
           pass, err := getPasswordFromPlugin(partition, kcryptConfig)
           return pass, true, err  // Keep keyslot for remote KMS
           
       case PassphraseSourceLocalTPM:
           // Read/generate from TPM NV memory
           pass, err := getLocalTPMPassphrase(kcryptConfig)
           return pass, true, err  // Keep keyslot for local TPM
           
       case PassphraseSourceEphemeral:
           // Generate random passphrase
           return getRandomString(32), false, nil  // Don't keep keyslot
       }
   }
   
   // Helper: Get passphrase from local TPM NV memory
   func getLocalTPMPassphrase(config *bus.DiscoveryPasswordPayload) (string, error) {
       // Move logic from kcrypt-challenger/cmd/discovery/client/enc.go::localPass()
       // Read from TPM NV index, or generate and store if not exists
   }
   ```

2. **Refactor encryption function to use unified passphrase acquisition:**
   
   **Location:** `kairos-sdk/kcrypt/lock.go`
   
   ```go
   // Encrypt creates an encrypted LUKS partition
   // If bindPCRs is provided, also enrolls TPM PCR policy
   func Encrypt(
       label string,
       kcryptConfig *bus.DiscoveryPasswordPayload,
       passphraseSource PassphraseSource,
       bindPublicPCRs []string,  // nil = no PCR binding
       bindPCRs []string,         // nil = no PCR binding
       logger types.KairosLogger,
   ) error {
       // 1. Find partition
       part, b, err := findPartition(label)
       
       // 2. Get passphrase based on source
       passphrase, keepKeyslot, err := GetPassphrase(b, kcryptConfig, passphraseSource, logger)
       
       // 3. Create LUKS with passphrase
       device := fmt.Sprintf("/dev/%s", part)
       err = createLuks(device, passphrase, ...)
       
       // 4. If PCR binding requested, enroll TPM policy
       if len(bindPCRs) > 0 || len(bindPublicPCRs) > 0 {
           err = enrollPCRPolicy(device, passphrase, bindPublicPCRs, bindPCRs, logger)
       }
       
       // 5. Format partition
       err = formatLuks(device, b.Name, mapper, label, passphrase, logger)
       
       // 6. If ephemeral passphrase, wipe password keyslot
       if !keepKeyslot {
           err = wipePasswordKeyslot(device)
       }
       
       return nil
   }
   ```

3. **kairos-agent decides passphrase source:**
   
   ```go
   // In kairos-agent/internal/agent/hooks/finish.go
   
   func encryptPartition(partition string, c config.Config, isUKI bool) error {
       // Determine passphrase source
       var source kcrypt.PassphraseSource
       if c.KcryptConfig.ChallengerServer != "" || c.KcryptConfig.MDNS {
           source = kcrypt.PassphraseSourceRemote
       } else if isUKI {
           source = kcrypt.PassphraseSourceEphemeral
       } else {
           source = kcrypt.PassphraseSourceLocalTPM
       }
       
       // Determine PCR binding
       var bindPCRs, bindPublicPCRs []string
       if isUKI {
           bindPCRs = c.BindPCRs
           bindPublicPCRs = c.BindPublicPCRs
       }
       
       // Single encryption call!
       return kcrypt.Encrypt(partition, c.KcryptConfig, source, bindPublicPCRs, bindPCRs, c.Logger)
   }
   ```

---

## Current Problem Statement

The UKI and non-UKI installation paths for partition encryption are completely separate (see `internal/agent/hooks/finish.go:37-41`), but they perform very similar operations:

### Common Operations (Both Paths):
1. Unmount partitions to-be-encrypted
2. Backup OEM partition (contains user config)
3. Encrypt partitions
4. Unlock encrypted partitions
5. Restore OEM partition data (if OEM was encrypted)
6. Lock/unmount encrypted partitions

### Current Differences:

| Aspect | Non-UKI (`Encrypt()`) | UKI (`EncryptUKI()`) |
|--------|----------------------|---------------------|
| **Config Check** | ✅ Checks for `kcrypt` config block | ❌ Ignores `kcrypt` config |
| **Encryption Types** | • Remote KMS (via kcrypt-challenger)<br>• Local encryption | • Local with PCRs only |
| **Partitions** | User-specified via `c.Install.Encrypt` | Always OEM + PERSISTENT + user-specified |
| **Encryption Function** | `kcrypt.EncryptWithConfig()` | `kcrypt.EncryptWithPcrs()` |
| **Unlock Function** | `kcrypt.UnlockAllWithConfig(false, ...)` | `kcrypt.UnlockAll(true, ...)` |
| **OEM Backup** | ❌ No backup | ✅ Backs up OEM before encryption |
| **TPM Check** | ❌ No TPM check | ✅ Checks for TPM 2.0 device |
| **Systemd Version Check** | ❌ No check | ✅ Requires systemd >= 252 |

### Main Issue:
**UKI mode doesn't support remote KMS, but there's no technical reason preventing it.** The kcrypt-challenger supports TPM-based remote attestation, which would work perfectly with UKI.

---

## Proposed Solution

### High-Level Strategy:
Simplify the architecture by making kcrypt-challenger responsible only for remote KMS, while kairos-sdk handles all local encryption. Extract common operations into reusable methods.

### Unified Encryption Flow:

```
kairos-agent: encryptPartition(partition, config, isUKI)
  │
  ├─ 1. Prepare
  │   ├─ unmount partition
  │   └─ backup OEM if needed
  │
  ├─ 2. Get Passphrase
  │   └─ passphrase = getPassphrase(config, isUKI)
  │        │
  │        ├─ if config.kcrypt.challenger.server configured:
  │        │   └─ return getRemotePassphrase(config)  [via kcrypt-challenger plugin]
  │        │
  │        ├─ else if isUKI:
  │        │   └─ return generateEphemeralPassphrase()  [random, will be discarded]
  │        │
  │        └─ else:
  │            └─ return getLocalTPMPassphrase()  [from TPM NV memory]
  │
  ├─ 3. Create LUKS with passphrase
  │   └─ cryptsetup luksFormat --type luks2 device [with passphrase]
  │
  ├─ 4. If UKI mode: Enroll PCR policy
  │   └─ systemd-cryptenroll --tpm2-public-key=... --tpm2-pcrs=... device
  │       [with passphrase from step 2]
  │
  ├─ 5. Format partition
  │   ├─ unlock with passphrase
  │   ├─ mkfs.ext4
  │   └─ close
  │
  ├─ 6. If UKI mode: Wipe password keyslot
  │   └─ systemd-cryptenroll --wipe-slot=password device
  │       [now only TPM can unlock]
  │
  └─ 7. Cleanup
      ├─ restore OEM if needed
      └─ lock partitions
```

### Passphrase Sources:

| Source | When Used | Stored Where | Unlock Method |
|--------|-----------|--------------|---------------|
| **Remote KMS** | `kcrypt.challenger.server` configured | Remote server | Retrieve from server via plugin |
| **Local TPM NV** | No remote config, non-UKI | TPM NV memory (index 0x1500000) | Read from TPM NV |
| **Ephemeral** | No remote config, UKI mode | Nowhere (discarded) | N/A - TPM policy unlocks |

### Key Insight:

**All encryption uses the same LUKS creation flow.** The only differences are:
1. Where the passphrase comes from (remote, local TPM, or ephemeral)
2. Whether PCR policy is added (UKI mode only)
3. Whether password keyslot is wiped (UKI mode only)

### Architecture Overview:

```
Finish.Run()
  └─> prepareForEncryption()  [COMMON]
       ├─> unmountPartitions()
       ├─> backupOEMPartition()
       └─> loadKcryptConfig()
  └─> encryptPartitions()     [BRANCHING POINT]
       ├─> encryptWithRemoteKMS()    [Remote attestation]
       ├─> encryptWithLocalPCRs()    [Local TPM with PCRs]
       └─> encryptWithLocalKey()     [Local encryption]
  └─> postEncryption()        [COMMON]
       ├─> unlockPartitions()
       ├─> waitForPartitions()
       ├─> restoreOEMPartition()
       └─> lockPartitions()
```

---

## Updated Architecture with Plugin Consolidation

### Current Architecture (BEFORE):

```
┌─────────────────────────────────────────────────────┐
│                  kairos-agent                        │
│  ┌──────────────────────┬────────────────────────┐  │
│  │   Non-UKI Path       │      UKI Path          │  │
│  │                      │                        │  │
│  │  Encrypt()           │   EncryptUKI()         │  │
│  │    │                 │      │                 │  │
│  │    ├─ unmount        │      ├─ unmount        │  │
│  │    │                 │      ├─ backup OEM     │  │
│  │    ├─ kcrypt.        │      ├─ kcrypt.        │  │
│  │    │   EncryptWith   │      │   EncryptWith   │  │
│  │    │   Config()      │      │   Pcrs()        │  │
│  │    │   │             │      │   │             │  │
│  │    │   └─> Plugin    │      │   └─> Direct    │  │
│  │    │       Bus       │      │       kairos-   │  │
│  │    │                 │      │       sdk call  │  │
│  │    ├─ unlock         │      ├─ unlock         │  │
│  │    └─ wait           │      ├─ restore OEM    │  │
│  │                      │      └─ unmount        │  │
│  └──────────────────────┴────────────────────────┘  │
└─────────────────────────────────────────────────────┘
           │                           │
           v                           v
┌────────────────────┐      ┌──────────────────────┐
│ kcrypt-challenger  │      │   systemd-          │
│     plugin         │      │   cryptenroll       │
│                    │      │   (direct call)     │
│ • Remote KMS       │      │                     │
│ • Local TPM-NV     │      │ • PCR binding       │
└────────────────────┘      └──────────────────────┘

❌ Problems:
  - Duplicate code in two paths
  - UKI can't use remote KMS
  - Direct systemd call bypasses plugin
```

### New Architecture (AFTER):

```
┌───────────────────────────────────────────────────────────┐
│                     kairos-agent                          │
│                                                           │
│            UNIFIED PATH (UKI & non-UKI)                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │  encryptPartitions(isUKI)                           │ │
│  │    │                                                 │ │
│  │    ├─ prepareForEncryption()                        │ │
│  │    │   ├─ unmountPartitions()                       │ │
│  │    │   ├─ backupOEMPartition() [if needed]          │ │
│  │    │   └─ loadKcryptConfig()                        │ │
│  │    │                                                 │ │
│  │    ├─ FOR EACH partition:                           │ │
│  │    │   └─ encryptViaPlugin(partition, config)       │ │
│  │    │                                                 │ │
│  │    └─ postEncryption()                              │ │
│  │        ├─ unlockPartitions()                        │ │
│  │        ├─ waitForPartitions()                       │ │
│  │        ├─ restoreOEMPartition() [if needed]         │ │
│  │        └─ lockPartitions()                          │ │
│  └─────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────┘
                           │
                           v  [Plugin Bus]
┌───────────────────────────────────────────────────────────┐
│              kcrypt-challenger plugin                     │
│                                                           │
│  EncryptPartition(payload)                                │
│    │                                                      │
│    ├─ if payload.ChallengerServer != ""                  │
│    │   └─> encryptWithRemoteKMS()                        │
│    │       └─> TPM attestation → server → passphrase     │
│    │                                                      │
│    ├─ else if payload.BindPCRs != nil                    │
│    │   └─> encryptWithSystemdPolicy()                    │
│    │       └─> Call kairos-sdk.EncryptWithPcrs()         │
│    │           └─> systemd-cryptenroll with PCR binding  │
│    │                                                      │
│    └─ else                                               │
│        └─> encryptWithLocalTPM()                         │
│            └─> TPM NV memory passphrase storage          │
└───────────────────────────────────────────────────────────┘

✅ Benefits:
  - Single code path in kairos-agent
  - UKI gains remote KMS support
  - Plugin owns all encryption logic
  - Easy to add new methods in plugin
  - Backwards compatible
```

### Comparison Table:

| Feature | Before (Non-UKI) | Before (UKI) | After (Both) |
|---------|------------------|--------------|--------------|
| **Code Path** | Separate | Separate | ✅ Unified |
| **Remote KMS** | ✅ Yes | ❌ No | ✅ Yes |
| **Local TPM-NV** | ✅ Yes | ❌ No | ✅ Yes |
| **Local PCR** | ❌ No | ✅ Yes | ✅ Yes |
| **Plugin Used** | ✅ Yes | ❌ No | ✅ Yes |
| **OEM Backup** | ❌ No | ✅ Yes | ✅ Yes |
| **Code Location** | Split | Split | ✅ Unified |

### Benefits of This Approach:

1. **Single Entry Point**: kairos-agent always calls plugin, no branching
2. **Plugin Flexibility**: kcrypt-challenger handles all encryption logic
3. **UKI Gets Remote KMS**: Naturally supported through unified path
4. **Independent Updates**: Plugin can be updated without rebuilding kairos-agent
5. **Testability**: Plugin logic can be tested independently

---

## Detailed Refactoring Steps

### Phase 0: Plugin Enhancement (CRITICAL)

This phase consolidates ALL encryption logic into the kcrypt-challenger plugin.

#### 0.1 Extend Plugin Payload
**Location:** `/home/dimitris/workspace/kairos/kairos-sdk/kcrypt/bus/payload.go`

**Changes:**
```go
type DiscoveryPasswordPayload struct {
    Partition *block.Partition `json:"partition"`
    
    // Remote KMS configuration
    ChallengerServer string `json:"challenger_server,omitempty"`
    MDNS             bool   `json:"mdns,omitempty"`
    Certificate      string `json:"certificate,omitempty"`
    
    // Local TPM configuration (NV memory method)
    NVIndex          string `json:"nv_index,omitempty"`
    CIndex           string `json:"c_index,omitempty"`
    TPMDevice        string `json:"tpm_device,omitempty"`
    
    // PCR binding configuration (systemd-cryptenroll method)
    BindPCRs         []string `json:"bind_pcrs,omitempty"`
    BindPublicPCRs   []string `json:"bind_public_pcrs,omitempty"`
    
    // Encryption mode selection
    // If ChallengerServer is set: use remote KMS
    // Else if BindPCRs/BindPublicPCRs set: use systemd-cryptenroll
    // Else: use TPM NV memory
}
```

#### 0.2 Add Encryption Event to Plugin
**Location:** `/home/dimitris/workspace/kairos/kairos-sdk/kcrypt/bus/bus.go`

**Changes:**
```go
const EventEncryptPartition pluggable.EventType = "encrypt.partition"

// Update Manager initialization
var Manager = pluggable.NewManager([]pluggable.EventType{
    EventDiscoveryPassword,
    EventEncryptPartition,  // NEW
})
```

#### 0.3 Implement Encryption in Plugin
**Location:** `/home/dimitris/workspace/kairos/kcrypt-challenger/cmd/discovery/client/client.go`

**Add new method:**
```go
// EncryptPartition handles partition encryption with automatic method selection
func (c *Client) EncryptPartition(payload *bus.DiscoveryPasswordPayload) error {
    // Decision logic:
    // 1. If ChallengerServer set -> use remote KMS for passphrase
    // 2. Else if BindPCRs set -> use systemd-cryptenroll with PCR binding
    // 3. Else -> use local TPM NV memory
    
    if payload.ChallengerServer != "" {
        return c.encryptWithRemoteKMS(payload)
    } else if len(payload.BindPCRs) > 0 || len(payload.BindPublicPCRs) > 0 {
        return c.encryptWithSystemdPolicy(payload)
    } else {
        return c.encryptWithLocalTPM(payload)
    }
}
```

**New file:** `/home/dimitris/workspace/kairos/kcrypt-challenger/cmd/discovery/client/encrypt.go`
```go
package client

// encryptWithRemoteKMS uses challenger server for passphrase
func (c *Client) encryptWithRemoteKMS(payload *bus.DiscoveryPasswordPayload) error {
    // Get passphrase from remote KMS via attestation
    passphrase, err := c.GetPassphrase(payload.Partition, 30)
    if err != nil {
        return err
    }
    
    // Call kairos-sdk to do the actual LUKS encryption
    return kcrypt.EncryptWithPassphrase(payload.Partition.FilesystemLabel, passphrase, c.Logger)
}

// encryptWithSystemdPolicy uses systemd-cryptenroll with PCR binding
// This is the UKI method, moved from kairos-sdk/kcrypt/lock.go::luksifyMeasurements
func (c *Client) encryptWithSystemdPolicy(payload *bus.DiscoveryPasswordPayload) error {
    // This replicates the logic from luksifyMeasurements()
    // See kairos-sdk/kcrypt/lock.go:198-277
    
    // 1. Find partition
    // 2. Generate random ephemeral password
    // 3. Create LUKS with password
    // 4. Enroll TPM2 policy with systemd-cryptenroll
    // 5. Format partition
    // 6. Wipe password slot
    
    return kcrypt.EncryptWithPcrs(
        payload.Partition.FilesystemLabel,
        payload.BindPublicPCRs,
        payload.BindPCRs,
        c.Logger,
    )
}

// encryptWithLocalTPM uses TPM NV memory for passphrase
func (c *Client) encryptWithLocalTPM(payload *bus.DiscoveryPasswordPayload) error {
    // Generate or retrieve passphrase from TPM NV memory
    passphrase, err := localPass(c.Config)
    if err != nil {
        return err
    }
    
    // Call kairos-sdk to do the actual LUKS encryption
    return kcrypt.EncryptWithPassphrase(payload.Partition.FilesystemLabel, passphrase, c.Logger)
}
```

#### 0.4 Add EncryptWithPassphrase to kairos-sdk
**Location:** `/home/dimitris/workspace/kairos/kairos-sdk/kcrypt/lock.go`

**New function:**
```go
// EncryptWithPassphrase encrypts a partition with an explicit passphrase
// This is a lower-level function used by the plugin
func EncryptWithPassphrase(label string, passphrase string, logger types.KairosLogger, argsCreate ...string) error {
    // This is basically luksifyWithConfig but with passphrase already provided
    // Extracts the common LUKS creation logic
    
    part, b, err := findPartition(label)
    if err != nil {
        return err
    }
    
    device := fmt.Sprintf("/dev/%s", part)
    mapper := fmt.Sprintf("/dev/mapper/%s", b.Name)
    
    // Unmount if needed
    if err := unmountIfMounted(device, logger); err != nil {
        return err
    }
    
    // Create LUKS
    extraArgs := []string{"--uuid", uuid.NewV5(uuid.NamespaceURL, label).String()}
    extraArgs = append(extraArgs, "--label", label)
    extraArgs = append(extraArgs, argsCreate...)
    
    if err := createLuks(device, passphrase, extraArgs...); err != nil {
        return err
    }
    
    // Format and close
    return formatLuks(device, b.Name, mapper, label, passphrase, logger)
}
```

---

### Phase 1: Extract Common Pre-Encryption Operations

#### 1.1 Create `preparePartitionsForEncryption()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Unmount all partitions that will be encrypted

**Signature:**
```go
func preparePartitionsForEncryption(c config.Config, partitions []string) error
```

**Logic:**
- For each partition label in `partitions`:
  - Find device path using `blkid -L <label>`
  - Find all mount points using `findmnt`
  - Unmount each mount point
  - Sync and wait briefly

**Replaces:**
- Lines 127-148 in `Encrypt()`
- Lines 237-244 in `EncryptUKI()`

---

#### 1.2 Create `backupOEMPartition()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Backup OEM partition contents before encryption

**Signature:**
```go
func backupOEMPartition(c config.Config, oemLabel string) (backupPath string, cleanup func(), err error)
```

**Logic:**
- Mount OEM partition
- Create temporary directory
- Sync OEM data to temp directory
- Unmount OEM
- Return backup path and cleanup function

**Returns:**
- `backupPath`: Path to backup directory
- `cleanup`: Function to remove backup (call with `defer`)
- `err`: Any error encountered

**Replaces:**
- Lines 246-266 in `EncryptUKI()`

**New Usage:**
- Should also be used in non-UKI path when OEM is in encrypt list

---

#### 1.3 Create `restoreOEMPartition()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Restore OEM partition contents after encryption

**Signature:**
```go
func restoreOEMPartition(c config.Config, oemLabel string, backupPath string) error
```

**Logic:**
- Mount unlocked OEM partition
- Sync backup data back to OEM
- Unmount OEM

**Replaces:**
- Lines 321-335 in `EncryptUKI()`

---

#### 1.4 Create `determinePartitionsToEncrypt()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Determine which partitions need encryption based on mode

**Signature:**
```go
func determinePartitionsToEncrypt(c config.Config, isUKI bool) []string
```

**Logic:**
```go
partitions := c.Install.Encrypt
if isUKI {
    // UKI always encrypts OEM and PERSISTENT
    partitions = append([]string{constants.OEMLabel, constants.PersistentLabel}, partitions...)
}
// Deduplicate
return deduplicateStrings(partitions)
```

---

### Phase 2: Extract Common Post-Encryption Operations

#### 2.1 Create `unlockEncryptedPartitions()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Unlock all encrypted partitions after encryption

**Signature:**
```go
func unlockEncryptedPartitions(c config.Config, useTpm bool, kcryptConfig *kcrypt.DiscoveryPasswordPayload) error
```

**Logic:**
- Sync filesystem
- Call appropriate unlock function:
  - If `useTpm`: `kcrypt.UnlockAll(true, c.Logger)`
  - Else: `kcrypt.UnlockAllWithConfig(false, c.Logger, kcryptConfig)`

**Replaces:**
- Lines 162-163 in `Encrypt()`
- Lines 284-291 in `EncryptUKI()`

---

#### 2.2 Create `waitForUnlockedPartitions()` method
**Location:** `internal/agent/hooks/finish.go`

**Purpose:** Wait for encrypted partitions to appear after unlocking

**Signature:**
```go
func waitForUnlockedPartitions(c config.Config, partitions []string) error
```

**Logic:**
- For each partition:
  - Retry up to 10 times with exponential backoff
  - Check if partition exists with `blkid -L <label>`
  - Retry unlock if partition not found
  - Return error if partition not found after 10 retries

**Replaces:**
- Lines 165-188 in `Encrypt()`
- Lines 296-319 in `EncryptUKI()`

---

### Phase 3: Simplify kairos-agent Encryption Logic (SIMPLIFIED)

With Phase 0 completed, kairos-agent no longer needs strategy classes. It just needs to:
1. Build the payload with all config
2. Call the plugin
3. Let the plugin decide what to do

#### 3.1 Create `encryptViaPlugin()` method
**Location:** `internal/agent/hooks/finish.go`

```go
// encryptViaPlugin encrypts a single partition by delegating to kcrypt-challenger plugin
func encryptViaPlugin(c config.Config, partition string, kcryptConfig *kcrypt.DiscoveryPasswordPayload) error {
    c.Logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition via plugin")
    
    // Build payload with all available configuration
    payload := kcryptConfig
    if payload == nil {
        payload = &kcrypt.DiscoveryPasswordPayload{}
    }
    
    // Add partition info (will be resolved by plugin)
    // We pass the label, plugin will find the actual partition
    
    // Add PCR config if in UKI mode
    if internalutils.IsUki() {
        payload.BindPCRs = c.BindPCRs
        payload.BindPublicPCRs = c.BindPublicPCRs
    }
    
    // Marshal payload and call plugin via bus
    // The plugin will decide: remote KMS vs local PCR vs local TPM-NV
    _, err := kcrypt.EncryptWithConfig(partition, c.Logger, payload)
    return err
}
```

**Note:** This is much simpler than the strategy pattern because the plugin handles all the logic!

---

### Phase 4: Create Unified Encryption Flow

#### 4.1 Refactor `Encrypt()` and `EncryptUKI()` into unified method
**Location:** `internal/agent/hooks/finish.go`

**New Method:**
```go
func encryptPartitions(c config.Config, isUKI bool) error {
    // 1. Determine partitions to encrypt
    partitions := determinePartitionsToEncrypt(c, isUKI)
    if len(partitions) == 0 {
        return nil
    }
    
    // 2. Select encryption strategy
    strategy, err := selectEncryptionStrategy(c, isUKI)
    if err != nil {
        return fmt.Errorf("failed to select encryption strategy: %w", err)
    }
    
    c.Logger.Logger.Info().
        Str("strategy", fmt.Sprintf("%T", strategy)).
        Strs("partitions", partitions).
        Msg("Starting partition encryption")
    
    // 3. Validate strategy can be used
    if err := strategy.Validate(c); err != nil {
        return fmt.Errorf("encryption strategy validation failed: %w", err)
    }
    
    // 4. Check if OEM needs backup
    needsOEMBackup := containsString(partitions, constants.OEMLabel)
    var oemBackupPath string
    var cleanupBackup func()
    
    if needsOEMBackup {
        oemBackupPath, cleanupBackup, err = backupOEMPartition(c, constants.OEMLabel)
        if err != nil {
            return fmt.Errorf("failed to backup OEM: %w", err)
        }
        defer cleanupBackup()
    }
    
    // 5. Unmount partitions
    if err := preparePartitionsForEncryption(c, partitions); err != nil {
        return fmt.Errorf("failed to prepare partitions: %w", err)
    }
    
    // 6. Encrypt each partition
    for _, partition := range partitions {
        c.Logger.Logger.Info().Str("partition", partition).Msg("Encrypting partition")
        if err := strategy.Encrypt(c, partition); err != nil {
            return fmt.Errorf("failed to encrypt %s: %w", partition, err)
        }
        c.Logger.Logger.Info().Str("partition", partition).Msg("Successfully encrypted")
    }
    
    // 7. Unlock partitions
    if err := unlockEncryptedPartitions(c, strategy.RequiresTPM(), strategy.GetKcryptConfig()); err != nil {
        return fmt.Errorf("failed to unlock partitions: %w", err)
    }
    
    // 8. Wait for partitions to appear
    if err := waitForUnlockedPartitions(c, partitions); err != nil {
        return fmt.Errorf("failed waiting for unlocked partitions: %w", err)
    }
    
    // 9. Restore OEM if needed
    if needsOEMBackup {
        if err := restoreOEMPartition(c, constants.OEMLabel, oemBackupPath); err != nil {
            return fmt.Errorf("failed to restore OEM: %w", err)
        }
    }
    
    return nil
}
```

#### 4.2 Update `Finish.Run()` to use unified method
**Location:** `internal/agent/hooks/finish.go`

**Replace lines 37-41 with:**
```go
if internalutils.IsUki() {
    err = encryptPartitions(c, true)
} else {
    err = encryptPartitions(c, false)
}
```

Or even simpler:
```go
err = encryptPartitions(c, internalutils.IsUki())
```

---

### Phase 5: Enhance kairos-sdk kcrypt Package (Optional)

#### 5.1 Add hybrid encryption support
**Location:** `/home/dimitris/workspace/kairos/kairos-sdk/kcrypt/lock.go`

**New Function:**
```go
// EncryptWithPcrsAndConfig encrypts with PCRs AND supports remote KMS for passphrase
// This enables UKI mode with remote attestation
func EncryptWithPcrsAndConfig(
    label string,
    publicKeyPcrs []string,
    pcrs []string,
    logger types.KairosLogger,
    kcryptConfig *bus.DiscoveryPasswordPayload,
    argsCreate ...string,
) error {
    // Similar to luksifyMeasurements but uses remote KMS for initial passphrase
    // instead of random local password
}
```

**Note:** This would enable a "best of both worlds" approach where:
- Passphrase is managed by remote KMS (kcrypt-challenger)
- Partition is also bound to PCRs for local TPM unlock
- Provides both remote attestation AND measured boot security

---

## Configuration Schema Changes

### Current Config (Non-UKI):
```yaml
install:
  encrypted_partitions:
    - COS_PERSISTENT

kcrypt:
  challenger:
    challenger_server: "https://kms.example.com"
    mdns: false
```

### Current Config (UKI):
```yaml
# OEM and PERSISTENT always encrypted
# No kcrypt config support
bind-pcrs: ["0", "7"]
bind-public-pcrs: ["11"]
```

### Proposed Unified Config:
```yaml
install:
  encrypted_partitions:
    - COS_PERSISTENT
    # In UKI mode, OEM and PERSISTENT are added automatically

# Encryption strategy auto-selected:
# 1. If kcrypt.challenger exists → Remote KMS
# 2. Else if UKI mode → Local PCRs
# 3. Else → Local key
kcrypt:
  challenger:
    challenger_server: "https://kms.example.com"
    mdns: false

# Only used if Local PCR strategy is selected
bind-pcrs: ["0", "7"]
bind-public-pcrs: ["11"]
```

---

## Testing Strategy

### Test Cases:

1. **Non-UKI with Remote KMS**
   - Config: `kcrypt.challenger` with server
   - Expected: Uses `RemoteKMSStrategy`
   - Verify: Partitions encrypted with remote passphrase

2. **Non-UKI with Local Key**
   - Config: No `kcrypt.challenger`
   - Expected: Uses `LocalKeyStrategy`
   - Verify: Partitions encrypted with local passphrase

3. **UKI with Remote KMS** (NEW!)
   - Config: `kcrypt.challenger` with server
   - Expected: Uses `RemoteKMSStrategy`
   - Verify: OEM + PERSISTENT encrypted with remote passphrase

4. **UKI with Local PCRs**
   - Config: No `kcrypt.challenger`, has TPM
   - Expected: Uses `LocalPCRStrategy`
   - Verify: Partitions encrypted and bound to PCRs

5. **UKI without TPM**
   - Config: No TPM device
   - Expected: Error during strategy validation
   - Verify: Clear error message about missing TPM

6. **OEM Backup/Restore**
   - Config: OEM in encrypt list
   - Expected: OEM backed up before encryption, restored after
   - Verify: OEM contents preserved

---

## Migration Path

### Backwards Compatibility:
- ✅ Existing non-UKI configs work unchanged
- ✅ Existing UKI configs work unchanged
- ✅ New feature: UKI can now use remote KMS

### Breaking Changes:
- ❌ None! This is purely additive refactoring

---

## Implementation Order (UPDATED)

### Phase 0: Plugin Enhancement (FIRST - CRITICAL)
**Priority:** HIGH - This is the foundation for everything else

**Changes:**
1. **kairos-sdk/kcrypt/bus/payload.go**
   - Add PCR fields to `DiscoveryPasswordPayload`
   
2. **kairos-sdk/kcrypt/bus/bus.go**
   - Add `EventEncryptPartition` event type
   
3. **kairos-sdk/kcrypt/lock.go**
   - Add `EncryptWithPassphrase()` helper function
   
4. **kcrypt-challenger/cmd/discovery/client/client.go**
   - Add `EncryptPartition()` method
   - Handle new event type
   
5. **kcrypt-challenger/cmd/discovery/client/encrypt.go** (NEW FILE)
   - `encryptWithRemoteKMS()`
   - `encryptWithSystemdPolicy()`
   - `encryptWithLocalTPM()`

**Testing:**
- Unit tests for each encryption method
- Integration test: plugin handles all three modes
- Verify systemd-cryptenroll works in plugin context

**Risk:** Medium - New code but well isolated

---

### Phase 1: Common Pre-Encryption Operations
**Priority:** MEDIUM - Can be done in parallel with Phase 0

**Changes:** Same as original plan
- `preparePartitionsForEncryption()`
- `backupOEMPartition()`
- `restoreOEMPartition()`
- `determinePartitionsToEncrypt()`

**Risk:** Low - Pure refactoring

---

### Phase 2: Common Post-Encryption Operations
**Priority:** MEDIUM - Can be done in parallel with Phase 0

**Changes:** Same as original plan
- `unlockEncryptedPartitions()`
- `waitForUnlockedPartitions()`

**Risk:** Low - Pure refactoring

---

### Phase 3: Simplify kairos-agent (AFTER Phase 0)
**Priority:** HIGH - Depends on Phase 0

**Changes:**
- Remove strategy pattern (simpler than original plan!)
- Just build payload and call plugin
- Plugin decides everything

**Risk:** Low - Very simple changes

---

### Phase 4: Unified Flow (FINAL)
**Priority:** HIGH - Integration

**Changes:**
- Update `Encrypt()` and `EncryptUKI()` to use common functions
- Or replace both with single `encryptPartitions()` method
- Update `Finish.Run()` to use unified method

**Risk:** Medium - Integration point

---

### Phase 5: Documentation & Testing (LAST)
**Priority:** HIGH - Validation

**Changes:**
- Update user-facing documentation
- Add migration guide
- Comprehensive test suite
- Performance testing

**Risk:** Low - Documentation

---

## Success Criteria

- [ ] UKI mode supports remote KMS encryption
- [ ] Code duplication between `Encrypt()` and `EncryptUKI()` eliminated
- [ ] All existing test cases pass
- [ ] New test cases for UKI + remote KMS pass
- [ ] OEM backup/restore works in both modes
- [ ] Clear error messages for validation failures
- [ ] Documentation updated

---

## Decision Matrix for Plugin Implementation

### Q1: Should we keep both local encryption methods?

**Options:**
- **A) Keep both methods** (TPM NV for non-UKI, systemd-cryptenroll for UKI)
- **B) Standardize on systemd-cryptenroll** for both
- **C) Standardize on TPM NV** for both

**Recommendation: Option A**

**Rationale:**
- Backwards compatible
- Each method has advantages:
  - TPM NV: Works with older systemd, simpler
  - systemd-cryptenroll: Better UKI integration, signed policy support
- Plugin can maintain both code paths easily

---

### Q2: Should we move `luksifyMeasurements()` to plugin or call it from plugin?

**Options:**
- **A) Move code into plugin** (duplicate in kcrypt-challenger)
- **B) Keep in kairos-sdk, call from plugin**
- **C) Keep in kairos-sdk, make it a library function**

**Recommendation: Option B**

**Rationale:**
- Don't duplicate code
- kairos-sdk is already a library
- Plugin just orchestrates, doesn't need to own all implementations
- `EncryptWithPcrs()` can stay in kairos-sdk, plugin calls it

**Implementation:**
```go
// In kcrypt-challenger plugin
func (c *Client) encryptWithSystemdPolicy(payload *bus.DiscoveryPasswordPayload) error {
    // Just call kairos-sdk function
    return kcrypt.EncryptWithPcrs(
        payload.Partition.FilesystemLabel,
        payload.BindPublicPCRs,
        payload.BindPCRs,
        c.Logger,
    )
}
```

---

### Q3: Should we support hybrid encryption (PCRs + remote KMS)?

**Scenario:** Passphrase from remote KMS, but also bind to PCRs

**Options:**
- **A) Not now, maybe later**
- **B) Implement now**
- **C) Never - these are mutually exclusive**

**Recommendation: Option A**

**Rationale:**
- Not a current use case
- Can be added later without breaking changes
- Would need new field: `AlsoBindPCRs: true`
- Focus on consolidation first

---

### Q4: Should plugin fail or fallback if remote KMS unreachable?

**Options:**
- **A) Fail immediately** (current behavior)
- **B) Fallback to local encryption**
- **C) Configurable with flag**

**Recommendation: Option A**

**Rationale:**
- Security-first approach
- Fallback could be security issue (downgrades protection)
- User should fix KMS connectivity, not work around it
- Document recovery procedures

---

### Q5: How should plugin decide which method to use?

**Decision Logic:**
```go
func (c *Client) EncryptPartition(payload *bus.DiscoveryPasswordPayload) error {
    // Priority order:
    
    // 1. Remote KMS if configured (highest priority)
    if payload.ChallengerServer != "" || payload.MDNS {
        return c.encryptWithRemoteKMS(payload)
    }
    
    // 2. PCR binding if requested (UKI mode)
    if len(payload.BindPCRs) > 0 || len(payload.BindPublicPCRs) > 0 {
        return c.encryptWithSystemdPolicy(payload)
    }
    
    // 3. Default to local TPM NV
    return c.encryptWithLocalTPM(payload)
}
```

**Validation:**
- ✅ Remote KMS works for both UKI and non-UKI
- ✅ UKI without KMS uses PCR binding (current behavior)
- ✅ Non-UKI without KMS uses TPM NV (current behavior)
- ✅ Backwards compatible

---

## Open Questions (Lower Priority)

1. **Should non-UKI mode also always encrypt OEM + PERSISTENT?**
   - Pros: Consistency between modes
   - Cons: Breaking change for existing configs
   - Decision: Keep current behavior (user-specified only)

2. **Should we add a config option to explicitly choose encryption method?**
   ```yaml
   install:
     encryption_method: "remote-kms" | "local-pcr" | "local-tpm" | "auto"
   ```
   - Pros: Explicit control, easier debugging
   - Cons: More config complexity
   - Decision: Start with auto-selection, add explicit option if users request it

3. **Should we validate that systemd >= 252 before UKI PCR encryption?**
   - Current UKI code does this check
   - Should plugin also do this?
   - Decision: Yes, plugin should validate and return clear error

---

## Files to Modify

### kairos-agent repository:
- `internal/agent/hooks/finish.go` - Main refactoring
- `internal/agent/hooks/encryption.go` - New file for strategies
- `internal/agent/hooks/finish_test.go` - New tests

### kairos-sdk repository (optional):
- `kcrypt/lock.go` - Add hybrid encryption function

---

## Estimated Effort

- Phase 1: 4 hours
- Phase 2: 4 hours
- Phase 3: 8 hours
- Phase 4: 6 hours
- Testing: 8 hours
- Documentation: 2 hours

**Total: ~32 hours (4 days)**

---

## Next Steps - Action Items

### Immediate Actions (Before Starting Implementation):

1. **Review & Approve This Plan**
   - [ ] Read through the investigation findings
   - [ ] Review the decision matrix (Q1-Q5)
   - [ ] Approve the plugin consolidation approach
   - [ ] Discuss any concerns or alternatives

2. **Validate Assumptions**
   - [ ] Confirm `systemd-cryptenroll` can be called from plugin context
   - [ ] Test that plugin can access `/run/systemd/tpm2-*` files
   - [ ] Verify plugin has necessary permissions for TPM operations
   - [ ] Check if there are any edge cases we haven't considered

3. **Set Up Test Environment**
   - [ ] Create test VMs for both UKI and non-UKI modes
   - [ ] Set up kcrypt-challenger server for remote KMS testing
   - [ ] Prepare test configurations for all three encryption methods

### Implementation Sequence:

#### Week 1: Phase 0 - Plugin Enhancement
**Goal:** Make kcrypt-challenger plugin handle all encryption types

**Day 1-2:**
- [ ] Update `kairos-sdk/kcrypt/bus/payload.go` with PCR fields
- [ ] Add `EventEncryptPartition` to bus
- [ ] Write unit tests for payload changes

**Day 3-4:**
- [ ] Create `kcrypt-challenger/cmd/discovery/client/encrypt.go`
- [ ] Implement three encryption methods in plugin
- [ ] Add `EncryptWithPassphrase()` helper to kairos-sdk

**Day 5:**
- [ ] Integration testing of plugin
- [ ] Test all three encryption modes
- [ ] Fix any issues found

#### Week 2: Phase 1-2 - Common Operations
**Goal:** Extract reusable functions in kairos-agent

**Day 1-2:**
- [ ] Implement `preparePartitionsForEncryption()`
- [ ] Implement `backupOEMPartition()` / `restoreOEMPartition()`
- [ ] Implement `determinePartitionsToEncrypt()`
- [ ] Write unit tests

**Day 3:**
- [ ] Implement `unlockEncryptedPartitions()`
- [ ] Implement `waitForUnlockedPartitions()`
- [ ] Write unit tests

**Day 4-5:**
- [ ] Test extracted functions in isolation
- [ ] Ensure backwards compatibility

#### Week 3: Phase 3-4 - Integration
**Goal:** Wire everything together

**Day 1-2:**
- [ ] Create unified `encryptPartitions()` method
- [ ] Update `Finish.Run()` to use unified method
- [ ] Remove old `Encrypt()` and `EncryptUKI()` methods

**Day 3-4:**
- [ ] End-to-end testing
  - [ ] Non-UKI with remote KMS
  - [ ] Non-UKI with local TPM-NV
  - [ ] UKI with remote KMS (NEW!)
  - [ ] UKI with local PCR binding
- [ ] Performance testing

**Day 5:**
- [ ] Fix any issues
- [ ] Code review

#### Week 4: Phase 5 - Documentation & Release
**Goal:** Document changes and prepare for release

**Day 1-2:**
- [ ] Update documentation
- [ ] Write migration guide
- [ ] Update examples in kcrypt-challenger README

**Day 3:**
- [ ] Final testing round
- [ ] Verify all test cases pass

**Day 4-5:**
- [ ] Create PR with detailed description
- [ ] Address review comments
- [ ] Merge and tag release

### Success Criteria Checklist:

- [ ] UKI mode can use remote KMS encryption
- [ ] Non-UKI mode continues to work (backwards compatible)
- [ ] UKI mode continues to work with local PCR binding
- [ ] All existing tests pass
- [ ] New tests cover all three encryption methods
- [ ] OEM backup/restore works in both modes
- [ ] Code duplication eliminated
- [ ] Clear error messages for misconfigurations
- [ ] Documentation updated

### Rollback Plan:

If issues are discovered after merge:
1. Revert to previous behavior is straightforward (old code still in git)
2. Plugin changes are backwards compatible (old kairos-agent still works)
3. Can disable new unified path with feature flag if needed

### Communication Plan:

- [ ] Announce refactoring in Kairos community channels
- [ ] Highlight new UKI + remote KMS capability
- [ ] Document any configuration changes needed
- [ ] Provide support during migration period
