package identity_test

import (
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
)

// Un compte existe avec ou sans nom de profil, et c'est son existence qui
// compte : sans elle, un compte réel sans nom affiché passerait pour un slug.
func TestUnCompteSansNomExisteQuandMeme(t *testing.T) {
	client, _ := monter(t)
	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)

	personne, existe, err := resolveur.Account("jlpicard")
	if err != nil || !existe || personne.FullName != "Jean-Luc Picard" {
		t.Fatalf("jlpicard = %+v, %v, %v", personne, existe, err)
	}
	personne, existe, err = resolveur.Account("aminata-d")
	if err != nil || !existe || personne.Username != "aminata-d" || personne.FullName != "" {
		t.Fatalf("aminata-d = %+v, %v, %v", personne, existe, err)
	}
	if _, existe, err = resolveur.Account("william-riker"); err != nil || existe {
		t.Fatalf("un slug de nom ne doit pas passer pour un compte : %v, %v", existe, err)
	}
}

// Ce que GitHub a répondu se mémorise, absence comprise : rouvrir l'annuaire
// ne doit pas redemander chaque compte.
func TestLaReponseDeGitHubSeMemorise(t *testing.T) {
	client, serveur := monter(t)
	stockage := cache.NewIn(t.TempDir(), true)
	for _, login := range []string{"jlpicard", "william-riker"} {
		if _, _, err := identity.New(client, stockage, 4).Account(login); err != nil {
			t.Fatal(err)
		}
	}
	avant := serveur.State.CallCount("/users/")

	autre := identity.New(client, stockage, 4)
	if _, existe, _ := autre.Account("JLPicard"); !existe {
		t.Error("la casse du compte ne doit pas faire oublier la réponse")
	}
	if _, existe, _ := autre.Account("william-riker"); existe {
		t.Error("l'absence mémorisée doit rester une absence")
	}
	if apres := serveur.State.CallCount("/users/"); apres != avant {
		t.Errorf("%d appel(s) de plus pour des comptes déjà demandés", apres-avant)
	}
}
