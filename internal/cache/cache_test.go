package cache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
)

func TestSetGet(t *testing.T) {
	c := cache.NewIn(t.TempDir(), true)
	depots := []groups.RepoInfo{{Name: "tp1-a", Private: true}, {Name: "tp1-b"}}
	c.Set(cache.ReposKey("ACME"), depots)

	var relu []groups.RepoInfo
	if !c.Get(cache.ReposKey("acme"), cache.ReposTTL, &relu) {
		t.Fatal("la valeur devrait être en cache (clé insensible à la casse)")
	}
	if len(relu) != 2 || relu[0].Name != "tp1-a" {
		t.Fatalf("relu = %+v", relu)
	}
}

// Une entrée périmée reste lisible pour qui le demande explicitement, avec son
// âge : c'est ce qu'on montre le temps de la relire.
func TestPeekLitUneEntreePerimeeEtDitSonAge(t *testing.T) {
	c := cache.NewIn(t.TempDir(), true)
	c.Set("profile:jlpicard", "Jean-Luc Picard")

	var nom string
	if c.Get("profile:jlpicard", -time.Second, &nom) {
		t.Fatal("Get ne doit pas servir une entrée périmée")
	}
	age, trouve := c.Peek("profile:jlpicard", &nom)
	if !trouve || nom != "Jean-Luc Picard" {
		t.Fatalf("Peek = %q, %v", nom, trouve)
	}
	if age < 0 || age > time.Minute {
		t.Errorf("âge = %v", age)
	}
	if _, trouve := c.Peek("profile:absent", &nom); trouve {
		t.Error("une clé absente ne doit rien rendre")
	}
	if _, trouve := cache.NewIn(t.TempDir(), false).Peek("profile:jlpicard", &nom); trouve {
		t.Error("un cache désactivé ne doit rien rendre")
	}
}

func TestGetPerime(t *testing.T) {
	dossier := t.TempDir()
	c := cache.NewIn(dossier, true)
	c.Set("profile:jlpicard", "Jean-Luc Picard")

	var nom string
	if !c.Get("profile:jlpicard", time.Hour, &nom) {
		t.Fatal("valeur absente")
	}
	if c.Get("profile:jlpicard", -time.Second, &nom) {
		t.Error("une entrée périmée ne doit pas être servie")
	}
}

func TestPersistanceEntreDeuxOuvertures(t *testing.T) {
	dossier := t.TempDir()
	premier := cache.NewIn(dossier, true)
	premier.Set("profile:emilie-cote", "Émilie Côté")

	second := cache.NewIn(dossier, true)
	var nom string
	if !second.Get("profile:emilie-cote", cache.ProfileTTL, &nom) || nom != "Émilie Côté" {
		t.Fatalf("relecture = %q", nom)
	}
}

func TestCacheDesactive(t *testing.T) {
	dossier := t.TempDir()
	c := cache.NewIn(dossier, false)
	c.Set("profile:x", "Nom")
	var nom string
	if c.Get("profile:x", cache.ProfileTTL, &nom) {
		t.Error("un cache désactivé ne doit rien servir")
	}
	if _, err := os.Stat(filepath.Join(dossier, "cache.json")); !os.IsNotExist(err) {
		t.Error("un cache désactivé ne doit rien écrire")
	}
}

func TestPermissionsDuFichier(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissions POSIX")
	}
	dossier := t.TempDir()
	c := cache.NewIn(dossier, true)
	c.Set("k", "v")
	info, err := os.Stat(c.Path())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("permissions %o, attendu 600", mode)
	}
}

func TestSetManyEtStats(t *testing.T) {
	c := cache.NewIn(t.TempDir(), true)
	c.SetMany(map[string]any{
		cache.ProfileKey("a"): "Personne A",
		cache.ProfileKey("b"): "", // un profil sans nom est mémorisé aussi
	})
	nombre, taille := c.Stats()
	if nombre != 2 || taille == 0 {
		t.Fatalf("Stats = %d, %d", nombre, taille)
	}
	var vide string
	if !c.Get(cache.ProfileKey("b"), cache.ProfileTTL, &vide) || vide != "" {
		t.Error("un nom vide doit rester mémorisé")
	}
	if description := c.Describe(); description == "vide" {
		t.Errorf("Describe = %q", description)
	}
}

func TestForgetEtClear(t *testing.T) {
	c := cache.NewIn(t.TempDir(), true)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Forget("a")
	var valeur int
	if c.Get("a", time.Hour, &valeur) {
		t.Error("l'entrée oubliée ne doit plus être servie")
	}
	if supprimees := c.Clear(); supprimees != 1 {
		t.Errorf("Clear = %d, attendu 1", supprimees)
	}
	if _, err := os.Stat(c.Path()); !os.IsNotExist(err) {
		t.Error("le fichier de cache doit disparaître")
	}
	if description := c.Describe(); description != "vide" {
		t.Errorf("Describe = %q", description)
	}
}

func TestFichierCorrompu(t *testing.T) {
	dossier := t.TempDir()
	if err := os.WriteFile(filepath.Join(dossier, "cache.json"), []byte("pas du JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := cache.NewIn(dossier, true)
	var valeur string
	if c.Get("k", time.Hour, &valeur) {
		t.Error("un cache corrompu doit se comporter comme un cache vide")
	}
	c.Set("k", "v")
	if !c.Get("k", time.Hour, &valeur) || valeur != "v" {
		t.Error("le cache doit redevenir utilisable après corruption")
	}
}

func TestNeConserveQueLesChampsUtiles(t *testing.T) {
	dossier := t.TempDir()
	c := cache.NewIn(dossier, true)
	c.Set(cache.ReposKey("acme"), []groups.RepoInfo{
		{Name: "tp1-a", Private: true, HTMLURL: "https://github.com/acme/tp1-a", PushedAt: "2026-08-01T00:00:00Z"},
	})
	contenu, err := os.ReadFile(c.Path())
	if err != nil {
		t.Fatal(err)
	}
	var brut map[string]struct {
		Value []map[string]json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(contenu, &brut); err != nil {
		t.Fatal(err)
	}
	champs := brut[cache.ReposKey("acme")].Value[0]
	if len(champs) != 4 {
		t.Errorf("%d champ(s) conservé(s) : %+v", len(champs), champs)
	}
}
