package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bartkleypas/please/internal/config"
	"github.com/bartkleypas/please/internal/storage"
	_ "modernc.org/sqlite"
)

func runVault(args []string) {
	if len(args) == 0 {
		printVaultUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "decrypt":
		runVaultDecrypt(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown vault command: %q\n", args[0])
		printVaultUsage()
		os.Exit(1)
	}
}

func printVaultUsage() {
	fmt.Println("Usage: please vault <command> [options]")
	fmt.Println("\nCommands:")
	fmt.Println("  decrypt   Decrypt all encrypted records in a SQLite vault")
	fmt.Println("\nOptions for decrypt:")
	fmt.Println("  -v, --vault string    Path to vault database (defaults to active workspace or global vault)")
	fmt.Println("  -o, --out string      Path to output decrypted database (default: decrypt in-place with .bak)")
	fmt.Println("  -k, --key string      Encryption key (default: from global ~/.please/config.json or env)")
	fmt.Println("  -c, --config string   Path to custom config.json")
}

func runVaultDecrypt(args []string) {
	fs := flag.NewFlagSet("please vault decrypt", flag.ExitOnError)
	vaultPath := fs.String("vault", "", "Path to the vault database file")
	fs.StringVar(vaultPath, "v", "", "Path to the vault database file (shorthand)")

	outPath := fs.String("out", "", "Output path for decrypted vault (default: in-place)")
	fs.StringVar(outPath, "o", "", "Output path for decrypted vault (shorthand)")

	keyFlag := fs.String("key", "", "Encryption key to use for decryption")
	fs.StringVar(keyFlag, "k", "", "Encryption key (shorthand)")

	configPath := fs.String("config", "", "Custom config file path")
	fs.StringVar(configPath, "c", "", "Custom config file path (shorthand)")

	_ = fs.Parse(args)

	// 1. Resolve Vault Path
	targetVault := *vaultPath
	if targetVault == "" {
		if wsDir, ok := config.GetWorkspacePleaseDir(); ok {
			candidate := filepath.Join(wsDir, "vault.db")
			if _, err := os.Stat(candidate); err == nil {
				targetVault = candidate
			}
		}
		if targetVault == "" {
			if globalDir, err := config.GetGlobalPleaseDir(); err == nil {
				candidate := filepath.Join(globalDir, "vault.db")
				if _, err := os.Stat(candidate); err == nil {
					targetVault = candidate
				}
			}
		}
	}

	if targetVault == "" {
		fmt.Fprintf(os.Stderr, "Error: no vault database found. Specify one with -v <path>\n")
		os.Exit(1)
	}

	absVault, err := filepath.Abs(targetVault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving vault path: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(absVault); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: vault file not found: %s\n", absVault)
		os.Exit(1)
	}

	// 2. Resolve Encryption Key
	key := resolveVaultKey(*keyFlag, *configPath)
	if key == "" {
		fmt.Fprintf(os.Stderr, "Error: no encryption key found. Specify one with -k <key>, via PLEASE_ENCRYPTION_KEY, or in ~/.please/config.json\n")
		os.Exit(1)
	}

	// 3. Determine Output Destination & Backup
	destFile := absVault
	backupCreated := ""

	if *outPath != "" {
		absOut, err := filepath.Abs(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error resolving output path: %v\n", err)
			os.Exit(1)
		}
		if absOut != absVault {
			destFile = absOut
			if err := copyFile(absVault, destFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error copying vault to %s: %v\n", destFile, err)
				os.Exit(1)
			}
		}
	}

	// If in-place decryption, create backup
	if destFile == absVault {
		backupPath := absVault + ".bak"
		if err := copyFile(absVault, backupPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to create backup at %s: %v\n", backupPath, err)
		} else {
			backupCreated = backupPath
		}
	}

	// 4. Perform In-Place Decryption
	nodesDecrypted, memsDecrypted, err := decryptSQLiteVault(destFile, key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Decryption error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🔓 Vault Decryption Complete!")
	fmt.Printf("   Database:           %s\n", destFile)
	if backupCreated != "" {
		fmt.Printf("   Backup Saved:       %s\n", backupCreated)
	}
	fmt.Printf("   Nodes Decrypted:    %d\n", nodesDecrypted)
	fmt.Printf("   Memories Decrypted: %d\n", memsDecrypted)
}

func resolveVaultKey(explicitKey, customConfig string) string {
	if explicitKey != "" {
		return explicitKey
	}
	if envKey := os.Getenv("PLEASE_ENCRYPTION_KEY"); envKey != "" {
		return envKey
	}
	if customConfig != "" {
		if cfg, err := config.LoadConfigFile(customConfig); err == nil && cfg.Server != nil && cfg.Server.EncryptionKey != "" {
			return cfg.Server.EncryptionKey
		}
	}
	// Check user global config directly
	if home, err := os.UserHomeDir(); err == nil {
		globalPath := filepath.Join(home, ".please", "config.json")
		if cfg, err := config.LoadConfigFile(globalPath); err == nil && cfg.Server != nil && cfg.Server.EncryptionKey != "" {
			return cfg.Server.EncryptionKey
		}
	}
	// Check default LoadConfig cascade
	if cfg, err := config.LoadConfig(); err == nil && cfg.Server != nil && cfg.Server.EncryptionKey != "" {
		return cfg.Server.EncryptionKey
	}
	return ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func decryptSQLiteVault(dbPath string, key string) (int, int, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Ensure WAL doesn't block updates
	_, _ = db.Exec("PRAGMA journal_mode = WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Decrypt Nodes
	nodeRows, err := tx.Query("SELECT id, content, thought, tool_calls, observations, images FROM nodes;")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query nodes: %w", err)
	}

	type nodeUpdate struct {
		id           string
		content      string
		thought      sql.NullString
		toolCalls    sql.NullString
		observations sql.NullString
		images       sql.NullString
	}

	var updates []nodeUpdate

	for nodeRows.Next() {
		var id, content string
		var thought, toolCalls, observations, images sql.NullString
		if err := nodeRows.Scan(&id, &content, &thought, &toolCalls, &observations, &images); err != nil {
			nodeRows.Close()
			return 0, 0, fmt.Errorf("failed to scan node: %w", err)
		}

		changed := false

		decContent, err := storage.DecryptField(content, key)
		if err != nil {
			nodeRows.Close()
			return 0, 0, fmt.Errorf("failed to decrypt node %s content: %w", id, err)
		}
		if decContent != content {
			content = decContent
			changed = true
		}

		if thought.Valid && strings.HasPrefix(thought.String, "enc:v1:") {
			decThought, err := storage.DecryptField(thought.String, key)
			if err != nil {
				nodeRows.Close()
				return 0, 0, fmt.Errorf("failed to decrypt node %s thought: %w", id, err)
			}
			thought.String = decThought
			changed = true
		}

		if toolCalls.Valid && strings.HasPrefix(toolCalls.String, "enc:v1:") {
			decToolCalls, err := storage.DecryptField(toolCalls.String, key)
			if err != nil {
				nodeRows.Close()
				return 0, 0, fmt.Errorf("failed to decrypt node %s tool_calls: %w", id, err)
			}
			toolCalls.String = decToolCalls
			changed = true
		}

		if observations.Valid && strings.HasPrefix(observations.String, "enc:v1:") {
			decObs, err := storage.DecryptField(observations.String, key)
			if err != nil {
				nodeRows.Close()
				return 0, 0, fmt.Errorf("failed to decrypt node %s observations: %w", id, err)
			}
			observations.String = decObs
			changed = true
		}

		if images.Valid && strings.HasPrefix(images.String, "enc:v1:") {
			decImages, err := storage.DecryptField(images.String, key)
			if err != nil {
				nodeRows.Close()
				return 0, 0, fmt.Errorf("failed to decrypt node %s images: %w", id, err)
			}
			images.String = decImages
			changed = true
		}

		if changed {
			updates = append(updates, nodeUpdate{
				id:           id,
				content:      content,
				thought:      thought,
				toolCalls:    toolCalls,
				observations: observations,
				images:       images,
			})
		}
	}
	nodeRows.Close()

	stmt, err := tx.Prepare("UPDATE nodes SET content = ?, thought = ?, tool_calls = ?, observations = ?, images = ? WHERE id = ?;")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to prepare node update statement: %w", err)
	}
	defer stmt.Close()

	for _, u := range updates {
		if _, err := stmt.Exec(u.content, u.thought, u.toolCalls, u.observations, u.images, u.id); err != nil {
			return 0, 0, fmt.Errorf("failed to update node %s: %w", u.id, err)
		}
	}

	// 2. Decrypt Memories
	memUpdates := make(map[string]string)
	memRows, err := tx.Query("SELECT id, content FROM memories WHERE content LIKE 'enc:v1:%';")
	if err == nil {
		for memRows.Next() {
			var id, encContent string
			if err := memRows.Scan(&id, &encContent); err == nil {
				if decContent, err := storage.DecryptField(encContent, key); err == nil {
					memUpdates[id] = decContent
				}
			}
		}
		memRows.Close()

		if len(memUpdates) > 0 {
			memStmt, err := tx.Prepare("UPDATE memories SET content = ? WHERE id = ?;")
			if err != nil {
				return len(updates), 0, fmt.Errorf("failed to prepare memory update statement: %w", err)
			}
			defer memStmt.Close()

			for id, dec := range memUpdates {
				if _, err := memStmt.Exec(dec, id); err != nil {
					return len(updates), 0, fmt.Errorf("failed to update memory %s: %w", id, err)
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("failed to commit decryption transaction: %w", err)
	}

	// Force checkpoint WAL back to the main DB file
	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")

	return len(updates), len(memUpdates), nil
}
