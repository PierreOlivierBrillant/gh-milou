package identity_test

import (
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
)

func monter(t *testing.T) (*ghapi.Client, *fakegh.Server) {
	t.Helper()
	serveur := fakegh.New(nil)
	t.Cleanup(serveur.Close)
	client, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton", BaseURL: serveur.URL(), Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, serveur
}

func TestNomsRetrouvesParLAPIPuisMisEnCache(t *testing.T) {
	client, serveur := monter(t)
	stockage := cache.NewIn(t.TempDir(), true)
	resolveur := identity.New(client, stockage, 4)

	paires := []identity.Pair{
		{Repo: "tp1-emilie-cote", Login: "emilie-cote"},
		{Repo: "tp1-jlpicard", Login: "jlpicard"},
		{Repo: "tp1-aminata-d", Login: "aminata-d"}, // profil sans nom
	}
	var progression int
	noms := resolveur.Resolve(paires, true, func(done, total int, repo string) {
		progression++
		if total != 3 {
			t.Errorf("total = %d", total)
		}
	})
	if noms["tp1-emilie-cote"] != "Émilie Côté" || noms["tp1-jlpicard"] != "Jean-Luc Picard" {
		t.Fatalf("noms = %+v", noms)
	}
	if noms["tp1-aminata-d"] != "" {
		t.Errorf("un profil sans nom doit rester vide : %q", noms["tp1-aminata-d"])
	}
	if progression != 3 {
		t.Errorf("progression = %d", progression)
	}
	appels := serveur.State.CallCount("/users/")
	if appels != 3 {
		t.Fatalf("%d appel(s) de profil", appels)
	}

	// Une seconde résolution ne doit plus rien demander : tout est en cache,
	// y compris le profil sans nom.
	autre := identity.New(client, stockage, 4)
	noms = autre.Resolve(paires, true, nil)
	if noms["tp1-jlpicard"] != "Jean-Luc Picard" {
		t.Errorf("cache non utilisé : %+v", noms)
	}
	if serveur.State.CallCount("/users/") != appels {
		t.Errorf("le cache n'a pas évité les appels : %d", serveur.State.CallCount("/users/"))
	}
}

func TestSansReseau(t *testing.T) {
	client, serveur := monter(t)
	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)
	paires := []identity.Pair{{Repo: "tp1-jlpicard", Login: "jlpicard"}}

	noms := resolveur.Resolve(paires, false, nil)
	if noms["tp1-jlpicard"] != "" {
		t.Errorf("noms = %+v", noms)
	}
	if serveur.State.CallCount("/users/") != 0 {
		t.Error("fetch=false interdit tout appel")
	}
	if manquants := resolveur.Missing(paires); len(manquants) != 1 {
		t.Errorf("manquants = %+v", manquants)
	}
}

func TestProfilIntrouvable(t *testing.T) {
	client, _ := monter(t)
	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)
	noms := resolveur.Resolve([]identity.Pair{{Repo: "tp1-fantome", Login: "fantome"}}, true, nil)
	if noms["tp1-fantome"] != "" {
		t.Errorf("noms = %+v", noms)
	}
}

func TestOwnersLitLeCompteDansLesAcces(t *testing.T) {
	client, serveur := monter(t)
	// Le nom ne dit pas où finit le travail : « kickmyb-firebase » en fait
	// partie, et le découper au jugé donnerait « firebase-Wasim7Zed ».
	serveur.State.AddRepo("acme", "kickmyb-firebase-Wasim7Zed", true)
	serveur.State.AddCollaborator("acme/kickmyb-firebase-Wasim7Zed", "Wasim7Zed", "push")
	// L'enseignant a accès à tout : il n'est l'indice de rien.
	serveur.State.AddCollaborator("acme/kickmyb-firebase-Wasim7Zed", "prof", "admin")

	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)
	trouves := resolveur.Owners("acme", []string{"kickmyb-firebase-Wasim7Zed"}, "prof", nil)
	if trouves["kickmyb-firebase-Wasim7Zed"].Login != "Wasim7Zed" {
		t.Fatalf("propriétaire = %+v", trouves["kickmyb-firebase-Wasim7Zed"])
	}
}

func TestOwnersCompteUneInvitationNonAcceptee(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "tp1-jlpicard", true)
	serveur.State.Invite("acme/tp1-jlpicard", "jlpicard", "push")

	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)
	trouves := resolveur.Owners("acme", []string{"tp1-jlpicard"}, "prof", nil)
	if trouves["tp1-jlpicard"].Login != "jlpicard" {
		t.Fatalf("propriétaire = %+v : une invitation en attente vaut un accès",
			trouves["tp1-jlpicard"])
	}
}

func TestOwnersSansAccesNeTranchePas(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "tp1-orphelin", true)

	resolveur := identity.New(client, cache.NewIn(t.TempDir(), true), 4)
	trouves := resolveur.Owners("acme", []string{"tp1-orphelin"}, "prof", nil)
	if trouves["tp1-orphelin"].Login != "" {
		t.Fatalf("propriétaire = %+v : rien n'y donne accès", trouves["tp1-orphelin"])
	}
}

func TestOwnersNeRedemandePasCeQuIlSait(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "tp1-jlpicard", true)
	serveur.State.AddCollaborator("acme/tp1-jlpicard", "jlpicard", "push")
	stockage := cache.NewIn(t.TempDir(), true)

	identity.New(client, stockage, 4).Owners("acme", []string{"tp1-jlpicard"}, "prof", nil)
	appels := serveur.State.CallCount("/collaborators")
	if appels == 0 {
		t.Fatal("aucun appel : le premier passage doit lire les accès")
	}

	// Un résolveur neuf, mais le même cache : plus rien ne part sur le réseau.
	trouves := identity.New(client, stockage, 4).
		Owners("acme", []string{"tp1-jlpicard"}, "prof", nil)
	if trouves["tp1-jlpicard"].Login != "jlpicard" {
		t.Fatalf("propriétaire = %+v", trouves["tp1-jlpicard"])
	}
	if serveur.State.CallCount("/collaborators") != appels {
		t.Fatalf("appels = %d, attendu %d : le cache devait répondre",
			serveur.State.CallCount("/collaborators"), appels)
	}
}

// Les accès se mémorisent comme les historiques : une fois lus, un écran les
// montre sans rien redemander — c'est ce qui rend le préchargement utile.
func TestAccesMemorisesSeRelisentSansReseau(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "a26.5n6.01.tp1.jlpicard", true)
	serveur.State.AddCollaborator("acme/a26.5n6.01.tp1.jlpicard", "jlpicard", "push")
	serveur.State.Invite("acme/a26.5n6.01.tp1.jlpicard", "ecote", "push")
	stockage := cache.NewIn(t.TempDir(), true)
	noms := []string{"a26.5n6.01.tp1.jlpicard"}

	// Avant toute lecture, « Cached » ne prétend rien : un dépôt qu'on n'a pas
	// regardé n'est pas un dépôt sans accès.
	if connus := identity.New(client, stockage, 4).
		Accesses("acme", noms, identity.Cached, nil); len(connus) != 0 {
		t.Fatalf("accès connus d'avance : %+v", connus)
	}

	lus := identity.New(client, stockage, 4).Accesses("acme", noms, identity.Fetch, nil)
	acces, inspecte := lus[noms[0]]
	if !inspecte || len(acces.Collaborators) != 1 || acces.Collaborators[0] != "jlpicard" {
		t.Fatalf("collaborateurs = %+v", acces)
	}
	if len(acces.Invitations) != 1 || acces.Invitations[0].Login != "ecote" ||
		acces.Invitations[0].ID == 0 {
		t.Fatalf("invitations = %+v : l'identifiant sert à l'annuler", acces.Invitations)
	}
	appels := serveur.State.CallCount("/collaborators")

	// Un résolveur neuf, mais la même mémoire : plus rien ne part sur le réseau.
	encore := identity.New(client, stockage, 4).Accesses("acme", noms, identity.Cached, nil)
	if len(encore) != 1 || len(encore[noms[0]].Invitations) != 1 {
		t.Fatalf("accès mémorisés = %+v", encore)
	}
	if serveur.State.CallCount("/collaborators") != appels {
		t.Fatalf("appels = %d, attendu %d : la mémoire devait répondre",
			serveur.State.CallCount("/collaborators"), appels)
	}
}

// « Refresh » relit tout : c'est ce que fait le geste explicite, et ce
// qu'« oublier » impose après un changement d'accès.
func TestAccesRelusSurDemandeEtApresUnChangement(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "a26.5n6.01.tp1.jlpicard", true)
	stockage := cache.NewIn(t.TempDir(), true)
	resolveur := identity.New(client, stockage, 4)
	noms := []string{"a26.5n6.01.tp1.jlpicard"}

	resolveur.Accesses("acme", noms, identity.Fetch, nil)
	serveur.State.AddCollaborator("acme/a26.5n6.01.tp1.jlpicard", "jlpicard", "push")

	// La mémoire d'avant le changement ne dit plus la vérité.
	if lus := resolveur.Accesses("acme", noms, identity.Fetch, nil); len(lus[noms[0]].Collaborators) != 0 {
		t.Fatalf("« Fetch » a relu ce qu'il savait déjà : %+v", lus)
	}
	if lus := resolveur.Accesses("acme", noms, identity.Refresh, nil); len(lus[noms[0]].Collaborators) != 1 {
		t.Fatalf("« Refresh » n'a pas relu : %+v", lus)
	}

	serveur.State.AddCollaborator("acme/a26.5n6.01.tp1.jlpicard", "ecote", "push")
	resolveur.ForgetAccess("acme", noms[0])
	acces, err := resolveur.AccessOf("acme", noms[0], identity.Fetch)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if len(acces.Collaborators) != 2 {
		t.Fatalf("collaborateurs = %+v : l'oubli devait forcer la relecture", acces)
	}
}

// Une mémoire d'une forme antérieure — une simple liste de comptes — ne dit
// plus ce qu'on lui demande : elle compte pour un dépôt qu'on n'a pas lu.
func TestUneMemoireDAccesAncienneEstIgnoree(t *testing.T) {
	client, serveur := monter(t)
	serveur.State.AddRepo("acme", "a26.5n6.01.tp1.jlpicard", true)
	serveur.State.AddCollaborator("acme/a26.5n6.01.tp1.jlpicard", "jlpicard", "push")
	stockage := cache.NewIn(t.TempDir(), true)
	stockage.Set(cache.AccessKey("acme", "a26.5n6.01.tp1.jlpicard"), []string{"jlpicard"})

	noms := []string{"a26.5n6.01.tp1.jlpicard"}
	if connus := identity.New(client, stockage, 4).
		Accesses("acme", noms, identity.Cached, nil); len(connus) != 0 {
		t.Fatalf("une forme antérieure a été prise pour une réponse : %+v", connus)
	}
	lus := identity.New(client, stockage, 4).Accesses("acme", noms, identity.Fetch, nil)
	if len(lus[noms[0]].Collaborators) != 1 {
		t.Fatalf("accès relus = %+v", lus)
	}
}
