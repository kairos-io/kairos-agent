# Partition Encryption Refactoring - Summary

## Proposed Architecture

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
