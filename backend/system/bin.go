package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	PruneBinRoot = "/host/home/nui/.prune/bin"
	MetadataFile = "/host/home/nui/.prune/bin/metadata.json"
)

type PruneMetadata struct {
	Timestamp time.Time         `json:"timestamp"`
	Mappings  map[string]string `json:"mappings"` // BinFolderName -> OriginalPath
}


// MoveToPruneBin moves selected assets to a temporary bin and stores metadata for restoration.
func MoveToPruneBin(selectedNames []string, allAssets []ProtectedAsset, dryRun bool) (int64, int64, error) {
	if err := os.MkdirAll(PruneBinRoot, 0755); err != nil {
		return 0, 0, err
	}

	// Load existing metadata or create new
	meta := PruneMetadata{
		Timestamp: time.Now(),
		Mappings:  make(map[string]string),
	}
	if data, err := os.ReadFile(MetadataFile); err == nil {
		json.Unmarshal(data, &meta)
	}

	var totalSize int64
	var movedCount int64

	// Create a lookup for selected assets
	selectedMap := make(map[string]bool)
	for _, name := range selectedNames {
		selectedMap[name] = true
	}

	for _, asset := range allAssets {
		// Unique key for matching: Name:Type
		key := asset.Name + ":" + asset.Type
		if selectedMap[key] {
			if asset.Path == "" || asset.Path == "unknown" {
				continue
			}

			// Generate a unique folder name in the bin to avoid collisions
			binFolderName := fmt.Sprintf("%d_%s_%s", time.Now().UnixNano(), asset.Type, asset.Name)
			binPath := filepath.Join(PruneBinRoot, binFolderName)

			if dryRun {
				LogActivity(fmt.Sprintf("[DRY RUN] Would move %s to %s", asset.Path, binPath))
			} else {
				LogActivity(fmt.Sprintf("Moving %s to Prune Bin...", asset.Name))
				if err := os.Rename(asset.Path, binPath); err != nil {
					// If rename fails across docker volumes (EXDEV), fallback to host 'mv'
					hostAsset := strings.TrimPrefix(asset.Path, "/host")
					hostBin := strings.TrimPrefix(binPath, "/host")
					if _, cmdErr := RunHostCommand(fmt.Sprintf("mv %q %q", hostAsset, hostBin)); cmdErr != nil {
						LogError(fmt.Sprintf("Failed to move %s: %v", asset.Name, cmdErr))
						continue
					}
				}
				meta.Mappings[binFolderName] = asset.Path
			}
			
			totalSize += asset.Size
			movedCount++
		}
	}

	if !dryRun && movedCount > 0 {
		data, _ := json.MarshalIndent(meta, "", "  ")
		os.WriteFile(MetadataFile, data, 0644)
	}

	return movedCount, totalSize, nil
}

// RestoreFromBin moves everything from the Prune Bin back to its original location.
func RestoreFromBin(dryRun bool) (int64, error) {
	data, err := os.ReadFile(MetadataFile)
	if err != nil {
		return 0, fmt.Errorf("no prune history found or bin is empty")
	}

	var meta PruneMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0, err
	}

	var restoredCount int64
	for binDir, originalPath := range meta.Mappings {
		binPath := filepath.Join(PruneBinRoot, binDir)
		
		if _, err := os.Stat(binPath); err != nil {
			continue // Already restored or missing
		}

		if dryRun {
			LogActivity(fmt.Sprintf("[DRY RUN] Would restore %s to %s", binPath, originalPath))
		} else {
			LogActivity(fmt.Sprintf("Restoring %s...", originalPath))
			fmt.Printf("[RESTORE] Restoring: %s\n", binDir)
			fmt.Printf("[RESTORE] Target Path: %s\n", originalPath)

			// Ensure parent dir exists
			os.MkdirAll(filepath.Dir(originalPath), 0755)
			if err := os.Rename(binPath, originalPath); err != nil {
				hostAsset := strings.TrimPrefix(originalPath, "/host")
				hostBin := strings.TrimPrefix(binPath, "/host")
				if _, cmdErr := RunHostCommand(fmt.Sprintf("mv %q %q", hostBin, hostAsset)); cmdErr != nil {
					fmt.Printf("[ERROR] Restore failed for %s: %v\n", binDir, cmdErr)
					LogError(fmt.Sprintf("Failed to restore %s: %v", originalPath, cmdErr))
					continue
				}
			}
			delete(meta.Mappings, binDir)
			restoredCount++
		}
	}

	if !dryRun {
		if len(meta.Mappings) == 0 {
			os.Remove(MetadataFile)
		} else {
			data, _ := json.MarshalIndent(meta, "", "  ")
			os.WriteFile(MetadataFile, data, 0644)
		}
	}

	return restoredCount, nil
}
