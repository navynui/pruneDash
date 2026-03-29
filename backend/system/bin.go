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

type PruneEntry struct {
	OriginalPath string `json:"originalPath"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
}

type PruneMetadata struct {
	Timestamp time.Time             `json:"timestamp"`
	Mappings  map[string]PruneEntry `json:"mappings"` // BinFolderName -> Entry
}

// GetBinAssets returns current items in the prune bin
func GetBinAssets() (map[string]PruneEntry, error) {
	data, err := os.ReadFile(MetadataFile)
	if err != nil {
		return nil, nil // Empty or non-existent
	}
	var meta PruneMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return meta.Mappings, nil
}

// MoveToPruneBin moves selected assets to a temporary bin and stores metadata for restoration.
func MoveToPruneBin(selectedNames []string, allAssets []ProtectedAsset, dryRun bool) (int64, int64, error) {
	if err := os.MkdirAll(PruneBinRoot, 0755); err != nil {
		return 0, 0, err
	}

	// Load existing metadata or create new
	meta := PruneMetadata{
		Timestamp: time.Now(),
		Mappings:  make(map[string]PruneEntry),
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
				if strings.Contains(asset.Path, "|") {
					// Handle multiple specific files grouped together (like loose fonts)
					os.MkdirAll(binPath, 0755)
					hostBin := strings.TrimPrefix(binPath, "/host")
					for _, p := range strings.Split(asset.Path, "|") {
						hostAsset := strings.TrimPrefix(p, "/host")
						if _, cmdErr := RunHostCommand(fmt.Sprintf("mv %q %q/", hostAsset, hostBin)); cmdErr != nil {
							LogError(fmt.Sprintf("Failed to move %s: %v", p, cmdErr))
						}
					}
				} else {
					if err := os.Rename(asset.Path, binPath); err != nil {
						// If rename fails across docker volumes (EXDEV), fallback to host 'mv'
						hostAsset := strings.TrimPrefix(asset.Path, "/host")
						hostBin := strings.TrimPrefix(binPath, "/host")
						if _, cmdErr := RunHostCommand(fmt.Sprintf("mv %q %q", hostAsset, hostBin)); cmdErr != nil {
							LogError(fmt.Sprintf("Failed to move %s: %v", asset.Name, cmdErr))
							continue
						}
					}
				}
				meta.Mappings[binFolderName] = PruneEntry{
					OriginalPath: asset.Path,
					Name:         asset.Name,
					Type:         asset.Type,
					Size:         asset.Size,
				}
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

// RestoreItem restores a single item from the bin
func RestoreItem(binDir string, dryRun bool) (string, error) {
	data, err := os.ReadFile(MetadataFile)
	if err != nil {
		return "", fmt.Errorf("no history found")
	}
	var meta PruneMetadata
	json.Unmarshal(data, &meta)

	entry, ok := meta.Mappings[binDir]
	if !ok {
		return "", fmt.Errorf("item not found in history")
	}

	binPath := filepath.Join(PruneBinRoot, binDir)
	if dryRun {
		LogActivity(fmt.Sprintf("[DRY RUN] Would restore %s to %s", binPath, entry.OriginalPath))
	} else {
		if strings.Contains(entry.OriginalPath, "|") {
			hostBin := strings.TrimPrefix(binPath, "/host")
			for _, p := range strings.Split(entry.OriginalPath, "|") {
				hostAsset := strings.TrimPrefix(p, "/host")
				os.MkdirAll(filepath.Dir(p), 0755)
				hostSubAsset := filepath.Join(hostBin, filepath.Base(p))
				RunHostCommand(fmt.Sprintf("mv %q %q", hostSubAsset, hostAsset))
			}
			os.RemoveAll(binPath)
		} else {
			os.MkdirAll(filepath.Dir(entry.OriginalPath), 0755)
			if err := os.Rename(binPath, entry.OriginalPath); err != nil {
				hostAsset := strings.TrimPrefix(entry.OriginalPath, "/host")
				hostBin := strings.TrimPrefix(binPath, "/host")
				RunHostCommand(fmt.Sprintf("mv %q %q", hostBin, hostAsset))
			}
		}
		delete(meta.Mappings, binDir)
		if len(meta.Mappings) == 0 {
			os.Remove(MetadataFile)
		} else {
			newData, _ := json.MarshalIndent(meta, "", "  ")
			os.WriteFile(MetadataFile, newData, 0644)
		}
	}
	return entry.Name, nil
}

// ConfirmItem permanently deletes/confirms an item (removes from bin UI)
func ConfirmItem(binDir string) error {
	data, err := os.ReadFile(MetadataFile)
	if err != nil {
		return err
	}
	var meta PruneMetadata
	json.Unmarshal(data, &meta)

	delete(meta.Mappings, binDir)
	// For "Confirmation" we actually delete the files from the bin now.
	binPath := filepath.Join(PruneBinRoot, binDir)
	os.RemoveAll(binPath)

	if len(meta.Mappings) == 0 {
		os.Remove(MetadataFile)
	} else {
		newData, _ := json.MarshalIndent(meta, "", "  ")
		os.WriteFile(MetadataFile, newData, 0644)
	}
	return nil
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
	for binDir, entry := range meta.Mappings {
		binPath := filepath.Join(PruneBinRoot, binDir)
		
		if _, err := os.Stat(binPath); err != nil {
			continue // Already restored or missing
		}

		if dryRun {
			LogActivity(fmt.Sprintf("[DRY RUN] Would restore %s to %s", binPath, entry.OriginalPath))
		} else {
			LogActivity(fmt.Sprintf("Restoring %s...", entry.OriginalPath))
			if strings.Contains(entry.OriginalPath, "|") {
				hostBin := strings.TrimPrefix(binPath, "/host")
				for _, p := range strings.Split(entry.OriginalPath, "|") {
					hostAsset := strings.TrimPrefix(p, "/host")
					os.MkdirAll(filepath.Dir(p), 0755)
					hostSubAsset := filepath.Join(hostBin, filepath.Base(p))
					RunHostCommand(fmt.Sprintf("mv %q %q", hostSubAsset, hostAsset))
				}
				os.RemoveAll(binPath)
			} else {
				os.MkdirAll(filepath.Dir(entry.OriginalPath), 0755)
				if err := os.Rename(binPath, entry.OriginalPath); err != nil {
					hostAsset := strings.TrimPrefix(entry.OriginalPath, "/host")
					hostBin := strings.TrimPrefix(binPath, "/host")
					RunHostCommand(fmt.Sprintf("mv %q %q", hostBin, hostAsset))
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
