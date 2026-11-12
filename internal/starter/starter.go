// Package starter lit un dossier local de fichiers de départ à déposer dans chaque dépôt.
package starter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Garde-fous : au-delà, mieux vaut un dépôt modèle qu'un envoi fichier par fichier.
const (
	MaxFileBytes     = 25 * 1024 * 1024
	MaxTotalBytes    = 50 * 1024 * 1024
	MaxFiles         = 500
	LargeBundleFiles = 40
	LargeBundleBytes = 5 * 1024 * 1024
	ModeFile         = "100644"
	ModeExecutable   = "100755"
)

// Dossiers jamais transmis : l'historique local n'a rien à faire dans le commit initial.
var excludedDirectories = map[string]bool{
	".git": true, "__pycache__": true, ".mypy_cache": true,
	".pytest_cache": true, ".venv": true,
}

var excludedFiles = map[string]bool{".DS_Store": true, "Thumbs.db": true}

// File est un fichier à inclure dans le commit initial.
type File struct {
	Path    string // chemin relatif, séparateurs POSIX
	Mode    string
	Content []byte
}

// Size renvoie la taille du fichier.
func (f File) Size() int { return len(f.Content) }

// Skipped décrit un fichier écarté et la raison de son exclusion.
type Skipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Bundle est le contenu retenu pour le commit initial, et ce qui a été écarté.
type Bundle struct {
	Root    string
	Files   []File
	Skipped []Skipped
}

// TotalBytes renvoie le poids total des fichiers retenus.
func (b *Bundle) TotalBytes() int {
	total := 0
	for _, file := range b.Files {
		total += file.Size()
	}
	return total
}

// IsLarge signale un envoi long : un dépôt modèle serait préférable.
func (b *Bundle) IsLarge() bool {
	return len(b.Files) > LargeBundleFiles || b.TotalBytes() > LargeBundleBytes
}

// Describe résume le contenu en une ligne.
func (b *Bundle) Describe() string {
	return valid.Plural(len(b.Files), "fichier") + ", " + HumanSize(b.TotalBytes())
}

// HumanSize met une taille en octets sous une forme lisible.
func HumanSize(size int) string {
	value := float64(size)
	if value < 1024 {
		return fmt.Sprintf("%.0f o", value)
	}
	value /= 1024
	if value < 1024 {
		return fmt.Sprintf("%.1f Kio", value)
	}
	return fmt.Sprintf("%.1f Mio", value/1024)
}

// isExecutable indique si le bit d'exécution est posé. Sous Windows, la notion
// n'existe pas : aucun fichier n'y est marqué 100755.
func isExecutable(info fs.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

// Load lit le dossier de départ et valide son contenu avant tout envoi.
func Load(path string) (*Bundle, error) {
	expanded, err := roster.ExpandPath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return nil, valid.Errorf("Dossier de départ introuvable : %s", expanded)
	}
	if !info.IsDir() {
		return nil, valid.Errorf("Dossier de départ : « %s » n'est pas un dossier.", expanded)
	}
	root, err := filepath.Abs(expanded)
	if err != nil {
		return nil, valid.Errorf("Dossier de départ illisible : %v", err)
	}

	bundle := &Bundle{Root: root}
	walkErr := filepath.WalkDir(root, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // un sous-dossier illisible ne doit pas tout interrompre
		}
		if entry.IsDir() {
			if current != root && excludedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return nil
		}
		relative = filepath.ToSlash(relative)

		if excludedFiles[entry.Name()] {
			bundle.Skipped = append(bundle.Skipped, Skipped{relative, "fichier système"})
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			bundle.Skipped = append(bundle.Skipped, Skipped{relative, "lien symbolique"})
			return nil
		}
		if !entry.Type().IsRegular() {
			bundle.Skipped = append(bundle.Skipped, Skipped{relative, "fichier spécial"})
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			bundle.Skipped = append(bundle.Skipped, Skipped{relative, "illisible"})
			return nil
		}
		if fileInfo.Size() > MaxFileBytes {
			return valid.Errorf("Dossier de départ : « %s » pèse %s, au-delà de la limite de %s.",
				relative, HumanSize(int(fileInfo.Size())), HumanSize(MaxFileBytes))
		}
		content, err := os.ReadFile(current)
		if err != nil {
			bundle.Skipped = append(bundle.Skipped, Skipped{relative, "illisible"})
			return nil
		}
		mode := ModeFile
		if isExecutable(fileInfo) {
			mode = ModeExecutable
		}
		bundle.Files = append(bundle.Files, File{Path: relative, Mode: mode, Content: content})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(bundle.Files, func(i, j int) bool { return bundle.Files[i].Path < bundle.Files[j].Path })
	sort.Slice(bundle.Skipped, func(i, j int) bool { return bundle.Skipped[i].Path < bundle.Skipped[j].Path })

	if len(bundle.Files) == 0 {
		return nil, valid.Errorf("Dossier de départ : « %s » ne contient aucun fichier transmissible.", root)
	}
	if len(bundle.Files) > MaxFiles {
		return nil, valid.Errorf(
			"Dossier de départ : %d fichiers, maximum %d. Utilisez plutôt un dépôt modèle (--template).",
			len(bundle.Files), MaxFiles)
	}
	if bundle.TotalBytes() > MaxTotalBytes {
		return nil, valid.Errorf(
			"Dossier de départ : %s au total, maximum %s. Utilisez plutôt un dépôt modèle.",
			HumanSize(bundle.TotalBytes()), HumanSize(MaxTotalBytes))
	}
	return bundle, nil
}

// NeedsWorkflowScope indique qu'un fichier vise .github/workflows : la portée
// « workflow » du jeton est alors nécessaire.
func (b *Bundle) NeedsWorkflowScope() bool {
	for _, file := range b.Files {
		if strings.HasPrefix(file.Path, ".github/workflows/") {
			return true
		}
	}
	return false
}
