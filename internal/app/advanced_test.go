package app_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
)

func TestOptionsAvanceesSansAuthentification(t *testing.T) {
	h := nouveau(t, nil)
	code, _ := h.script(
		"avance",       // Que voulez-vous faire ?
		"emplacements", // Afficher les emplacements
		"revenir",      // Retour au menu principal
		"quitter",      // Quitter
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Options avancées", "Réglages", "Cache", "Bilans")
	// Rien de tout cela n'exige de jeton : aucun appel réseau ne doit partir.
	if appels := h.State.AllCalls(); len(appels) != 0 {
		t.Errorf("appels réseau = %v", appels)
	}
}

func TestOptionsAvanceesViderLeCache(t *testing.T) {
	h := nouveau(t, nil)
	stockage := cache.NewIn(h.CacheDir, true)
	stockage.Set(cache.ReposKey("acme"), []string{"tp1-a"})

	code, _ := h.script("avance", "vider", "oui", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("1 entrée supprimée")
	if _, err := os.Stat(filepath.Join(h.CacheDir, "cache.json")); !os.IsNotExist(err) {
		t.Error("le fichier de cache aurait dû disparaître")
	}
}

func TestOptionsAvanceesCacheDejaVide(t *testing.T) {
	h := nouveau(t, nil)
	code, _ := h.script("avance", "vider", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Le cache est déjà vide")
}

func TestOptionsAvanceesPurgeAnnulee(t *testing.T) {
	h := nouveau(t, nil)
	stockage := cache.NewIn(h.CacheDir, true)
	stockage.Set("k", "v")

	code, _ := h.script("avance", "vider", "non", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Annulé")
	if _, err := os.Stat(filepath.Join(h.CacheDir, "cache.json")); err != nil {
		t.Error("le cache devait rester intact")
	}
}

func TestOptionsAvanceesOublierLesReglages(t *testing.T) {
	h := nouveau(t, nil)
	if err := os.WriteFile(h.Reglages, []byte(`{"org":"acme","assignment":"tp1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _ := h.script("avance", "reglages", "oui", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Réglages oubliés")
	if _, err := os.Stat(h.Reglages); !os.IsNotExist(err) {
		t.Error("le fichier de réglages aurait dû disparaître")
	}
}

func TestOptionsAvanceesAucunReglage(t *testing.T) {
	h := nouveau(t, nil)
	code, _ := h.script("avance", "reglages", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Aucun réglage mémorisé")
}

func TestMenuPrincipalQuitter(t *testing.T) {
	h := nouveau(t, nil)
	code, _ := h.script("quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if appels := h.State.AllCalls(); len(appels) != 0 {
		t.Errorf("quitter ne doit rien demander à GitHub : %v", appels)
	}
}

// L'écran des portées est le pendant, au terminal, de la boîte « Portées du
// jeton » des réglages généraux de l'interface web.
func TestOptionsAvanceesPorteesDuJeton(t *testing.T) {
	h := nouveau(t, nil)
	h.State.Scopes = "repo, read:org"
	h.Refresher = accordeLesPortees(h.State, nil)

	code, _ := h.script(
		"avance",
		"portees",
		"1,2,5", // repo, read:org et delete_repo
		"oui",   // générer un nouveau jeton
		"revenir",
		"quitter",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Portées du jeton", "delete_repo", "Jeton renouvelé")
	if !strings.Contains(h.State.Scopes, "delete_repo") {
		t.Errorf("portées accordées = %q", h.State.Scopes)
	}
}

func TestOptionsAvanceesPorteesDejaCompletes(t *testing.T) {
	h := nouveau(t, nil)
	// Les portées que le jeton d'essai porte déjà : « admin:org » n'en est pas.
	code, _ := h.script("avance", "portees", "1,2,4,5", "revenir", "quitter")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Le jeton porte déjà ces portées")
}

// Les mêmes portées se demandent sans question, en ligne de commande.
func TestRenouvellementDuJetonEnLigneDeCommande(t *testing.T) {
	h := nouveau(t, nil)
	h.State.Scopes = "repo, read:org"
	h.Refresher = accordeLesPortees(h.State, nil)
	h.Options.RefreshToken = true
	h.Options.Scopes = "delete_repo"

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Portées du jeton", "Jeton renouvelé")
	if !strings.Contains(h.State.Scopes, "delete_repo") {
		t.Errorf("portées accordées = %q", h.State.Scopes)
	}
	// Ce que le jeton portait déjà ne doit pas s'être perdu en chemin.
	if !strings.Contains(h.State.Scopes, "read:org") {
		t.Errorf("portées accordées = %q", h.State.Scopes)
	}
}

func TestRenouvellementDuJetonRefusePorteeInvalide(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.RefreshToken = true
	h.Options.Scopes = "--reset-scopes"
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
}
