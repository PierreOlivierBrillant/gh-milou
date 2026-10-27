// Package cache conserve sur disque les données GitHub peu changeantes
// (liste des dépôts d'une organisation, noms des profils).
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/brand"
)

// Durées de validité : la liste des dépôts bouge plus souvent qu'un nom de profil.
const (
	ReposTTL = 6 * time.Hour
	OrgsTTL  = 12 * time.Hour
	// Les équipes changent sous la main de qui enseigne — une composition
	// corrigée sur github.com doit se voir vite : elles se gardent moins
	// longtemps que les dépôts.
	TeamsTTL   = time.Hour
	ProfileTTL = 30 * 24 * time.Hour
	// AccessTTL est courte : un étudiant ajouté à un dépôt doit être vu le jour
	// même, là où un nom de profil ne change presque jamais.
	AccessTTL = 6 * time.Hour
	// HandinTTL est courte : ce qu'un étudiant vient de pousser doit se voir,
	// et c'est à la veille d'une date de remise qu'on regarde le plus souvent.
	HandinTTL = time.Hour
	// HistoryTTL borne ce qu'on garde d'un historique pour dater l'activité
	// d'un dépôt. Ce n'est pas l'âge qui le périme : c'est un envoi fait depuis
	// sa lecture, et le relevé le sait (« Handin.Covers »).
	HistoryTTL = 30 * 24 * time.Hour
	// RegistryTTL est longue à dessein : ce n'est pas le temps qui dit si le
	// registre a bougé, c'est le commit relevé sur sa branche. La péremption
	// n'est là que pour ne pas garder indéfiniment ce qui ne sert plus.
	RegistryTTL = 90 * 24 * time.Hour
	fileName    = "cache.json"
)

// ReposKey est la clé de la liste des dépôts d'une organisation.
func ReposKey(org string) string { return "repos:" + strings.ToLower(org) }

// TeamsKey est la clé des équipes d'une organisation.
func TeamsKey(org string) string { return "teams:" + strings.ToLower(org) }

// OrgsKey est la clé des organisations accessibles à un compte.
func OrgsKey(viewer string) string { return "orgs:" + strings.ToLower(viewer) }

// RegistryKey est la clé du registre des étudiants d'une organisation.
func RegistryKey(org string) string { return "registry:" + strings.ToLower(org) }

// ProfileKey est la clé du nom complet associé à un compte GitHub.
func ProfileKey(login string) string { return "profile:" + strings.ToLower(login) }

// AccessKey est la clé des comptes qui ont accès à un dépôt.
func AccessKey(org, repo string) string {
	return "access:" + strings.ToLower(org) + "/" + strings.ToLower(repo)
}

// HandinKey est la clé de ce que l'historique d'un dépôt dit d'une remise.
func HandinKey(org, repo string) string {
	return "handin:" + strings.ToLower(org) + "/" + strings.ToLower(repo)
}

// CrewKey est la clé des personnes qu'un dépôt d'équipe rassemble : ses
// équipes GitHub, ses collaborateurs, ses auteurs de commits.
func CrewKey(org, repo string) string {
	return "crew:" + strings.ToLower(org) + "/" + strings.ToLower(repo)
}

// Dir renvoie l'emplacement du cache, conforme au standard du système. Un
// cache laissé par l'ancien nom de l'outil est repris plutôt qu'abandonné.
func Dir() string {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return brand.AdoptLegacyDir(base)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return brand.AdoptLegacyDir(filepath.Join(".", ".cache"))
	}
	return brand.AdoptLegacyDir(base)
}

type entry struct {
	At    float64         `json:"at"`
	Value json.RawMessage `json:"value"`
}

// Cache est un stockage clé-valeur sur disque, avec péremption par entrée.
// Tout tient dans un seul fichier JSON, ce qui rend la purge triviale et évite
// d'éparpiller des centaines de petits fichiers.
type Cache struct {
	Directory string
	Enabled   bool

	mutex   sync.Mutex
	entries map[string]entry
	loaded  bool
}

// New ouvre le cache du système, actif ou non.
func New(enabled bool) *Cache {
	return &Cache{Directory: Dir(), Enabled: enabled}
}

// NewIn ouvre un cache dans un dossier donné (utile aux tests).
func NewIn(directory string, enabled bool) *Cache {
	return &Cache{Directory: directory, Enabled: enabled}
}

// Path renvoie le chemin du fichier de cache.
func (c *Cache) Path() string { return filepath.Join(c.Directory, fileName) }

func (c *Cache) load() map[string]entry {
	if c.loaded {
		return c.entries
	}
	c.loaded = true
	c.entries = map[string]entry{}
	content, err := os.ReadFile(c.Path())
	if err != nil {
		return c.entries
	}
	if err := json.Unmarshal(content, &c.entries); err != nil {
		c.entries = map[string]entry{}
	}
	return c.entries
}

func (c *Cache) save() {
	if !c.Enabled {
		return
	}
	payload, err := json.Marshal(c.entries)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.Directory, 0o700); err != nil {
		return // un cache indisponible ne doit jamais interrompre le travail
	}
	// Écriture atomique : un remplacement rate moins qu'une réécriture.
	temporary := c.Path() + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return
	}
	if err := os.Rename(temporary, c.Path()); err != nil {
		os.Remove(temporary)
		return
	}
	_ = os.Chmod(c.Path(), 0o600)
}

// Get lit une valeur encore valide et la décode dans target.
func (c *Cache) Get(key string, maxAge time.Duration, target any) bool {
	if !c.Enabled {
		return false
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	item, found := c.load()[key]
	if !found || len(item.Value) == 0 {
		return false
	}
	if time.Since(time.Unix(int64(item.At), 0)) > maxAge {
		return false
	}
	return json.Unmarshal(item.Value, target) == nil
}

// Peek lit une valeur quel que soit son âge, et rend cet âge. Ce qui a vieilli
// ne sert qu'à montrer quelque chose le temps d'aller chercher mieux : jamais à
// décider d'une écriture, qui doit partir de ce que Get accepte.
func (c *Cache) Peek(key string, target any) (time.Duration, bool) {
	if !c.Enabled {
		return 0, false
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	item, found := c.load()[key]
	if !found || len(item.Value) == 0 {
		return 0, false
	}
	if json.Unmarshal(item.Value, target) != nil {
		return 0, false
	}
	return time.Since(time.Unix(int64(item.At), 0)), true
}

// Set enregistre une valeur et l'horodate.
func (c *Cache) Set(key string, value any) {
	if !c.Enabled {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	c.load()[key] = entry{At: float64(time.Now().Unix()), Value: payload}
	c.save()
}

// SetMany enregistre plusieurs valeurs en une seule écriture.
func (c *Cache) SetMany(values map[string]any) {
	if !c.Enabled || len(values) == 0 {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	entries := c.load()
	stamp := float64(time.Now().Unix())
	for key, value := range values {
		payload, err := json.Marshal(value)
		if err != nil {
			continue
		}
		entries[key] = entry{At: stamp, Value: payload}
	}
	c.save()
}

// Forget oublie une entrée précise.
func (c *Cache) Forget(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	entries := c.load()
	if _, found := entries[key]; found {
		delete(entries, key)
		c.save()
	}
}

// Stats renvoie le nombre d'entrées et la taille du fichier, en octets.
func (c *Cache) Stats() (int, int64) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	count := len(c.load())
	var size int64
	if info, err := os.Stat(c.Path()); err == nil {
		size = info.Size()
	}
	return count, size
}

// Describe résume l'état du cache en une ligne lisible.
func (c *Cache) Describe() string {
	count, size := c.Stats()
	if count == 0 {
		return "vide"
	}
	c.mutex.Lock()
	recent := time.Duration(0)
	first := true
	for _, item := range c.load() {
		age := time.Since(time.Unix(int64(item.At), 0))
		if first || age < recent {
			recent, first = age, false
		}
	}
	c.mutex.Unlock()
	return fmt.Sprintf("%d entrée(s), %.1f Kio, plus récente il y a %s",
		count, float64(size)/1024, Age(recent))
}

// Clear vide le cache et renvoie le nombre d'entrées supprimées.
func (c *Cache) Clear() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	count := len(c.load())
	c.entries = map[string]entry{}
	_ = os.Remove(c.Path())
	return count
}

// Age met une durée sous une forme lisible.
func Age(duration time.Duration) string {
	seconds := duration.Seconds()
	switch {
	case seconds < 90:
		return fmt.Sprintf("%.0f s", seconds)
	case seconds < 5400:
		return fmt.Sprintf("%.0f min", seconds/60)
	case seconds < 172800:
		return fmt.Sprintf("%.0f h", seconds/3600)
	default:
		return fmt.Sprintf("%.0f j", seconds/86400)
	}
}
