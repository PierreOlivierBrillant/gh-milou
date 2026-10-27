package registry_test

import (
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
)

// magasin monte un faux GitHub et le registre qui s'y écrit.
func magasin(t *testing.T, state *fakegh.State) (*registry.Store, *fakegh.Server) {
	t.Helper()
	if state == nil {
		state = fakegh.NewState()
	}
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)

	client, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton-de-test", BaseURL: serveur.URL(),
		Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	return registry.New(client, "acme", nil), serveur
}

// clientDe monte un second client sur le même faux GitHub : c'est ce qu'il
// faut pour jouer un autre poste, qui ne partage ni mémoire ni cache.
func clientDe(t *testing.T, serveur *fakegh.Server) *ghapi.Client {
	t.Helper()
	client, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton-de-test", BaseURL: serveur.URL(),
		Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	return client
}

// Une organisation où l'on n'a rien écrit n'est pas une panne : elle a
// simplement un registre vide.
func TestRegistreAbsentSeLitVide(t *testing.T) {
	store, serveur := magasin(t, nil)
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if snapshot.Set.Len() != 0 || snapshot.Head != "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	// Lire n'écrit rien : le dépôt du registre n'est pas créé au passage.
	if noms := serveur.State.RepoNames("acme"); len(noms) != 0 {
		t.Errorf("dépôts créés par une simple lecture : %v", noms)
	}
}

// La première écriture crée le dépôt, privé, et y dépose de quoi l'expliquer.
func TestLaPremiereEcritureAmorceLeDepot(t *testing.T) {
	store, serveur := magasin(t, nil)
	set, err := store.Apply(registry.Learn(
		personne("Émilie Côté", "ecote"), personne("Jean-Luc Picard", "jlpicard")))
	if err != nil {
		t.Fatalf("Apply : %v", err)
	}
	if set.Len() != 2 {
		t.Fatalf("%d fiche(s)", set.Len())
	}

	depot := serveur.State.Repos["acme/"+registry.RepoName]
	if depot == nil {
		t.Fatal("le dépôt du registre n'a pas été créé")
	}
	if !depot.Private {
		t.Error("le registre porte des noms d'étudiants : il doit être privé")
	}
	fichiers := serveur.State.Files("acme/"+registry.RepoName, registry.Branch)
	if _, present := fichiers[registry.UsersFile]; !present {
		t.Fatalf("fichiers = %v", fichiers)
	}
	if lisezmoi := fichiers[registry.ReadmeFile]; !strings.Contains(lisezmoi, "privé") {
		t.Errorf("le LISEZMOI doit dire que le dépôt reste privé : %q", lisezmoi)
	}

	// Relu depuis GitHub, c'est bien ce qu'on y a mis.
	relu, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if relu.Set.Name("ecote") != "Émilie Côté" || relu.Head == "" {
		t.Fatalf("relu = %+v", relu)
	}
	if len(relu.Issues) != 0 {
		t.Errorf("soucis à la relecture : %v", relu.Issues)
	}
}

// Un changement qui ne change rien n'écrit rien : un commit sans effet salit
// l'historique sans rien apprendre à personne.
func TestUnChangementSansEffetNEcritRien(t *testing.T) {
	store, serveur := magasin(t, nil)
	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	commits := serveur.State.CallCount("POST /repos/acme/.cohorte/git/commits")

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	if apres := serveur.State.CallCount("POST /repos/acme/.cohorte/git/commits"); apres != commits {
		t.Errorf("%d commit(s) de plus pour un changement sans effet", apres-commits)
	}
}

// Deux personnes écrivent en même temps. Le refus de GitHub fait relire et
// rejouer celle qui arrive après, et aucune des deux ne perd son travail.
func TestDeuxEcrituresConcurrentesSeConserventToutesDeux(t *testing.T) {
	state := fakegh.NewState()
	store, serveur := magasin(t, state)

	// Le registre existe déjà : les deux écritures partent du même commit.
	if _, err := store.Apply(registry.Learn(personne("Prof Une", "prof"))); err != nil {
		t.Fatal(err)
	}

	// La barrière retient les deux écrivains le temps qu'ils aient tous deux
	// relevé la même tête. Sans elle, ils se suivraient sagement et le rejeu
	// ne serait jamais éprouvé.
	depart := fakegh.NewBarrier(2, 2*time.Second)
	state.Hook = func(request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/git/ref/heads/"+registry.Branch) {
			depart.Wait()
		}
	}

	// Chaque écrivain a son propre magasin : le verrou interne d'un même
	// magasin les sérialiserait avant même d'atteindre GitHub.
	second := registry.New(clientVers(t, serveur), "acme", nil)

	var groupe sync.WaitGroup
	echecs := make([]error, 2)
	groupe.Add(2)
	go func() {
		defer groupe.Done()
		_, echecs[0] = store.Apply(registry.Learn(personne("Émilie Côté", "ecote")))
	}()
	go func() {
		defer groupe.Done()
		_, echecs[1] = second.Apply(registry.Learn(personne("Jean-Luc Picard", "jlpicard")))
	}()
	groupe.Wait()

	for index, err := range echecs {
		if err != nil {
			t.Fatalf("écrivain %d : %v", index+1, err)
		}
	}
	if !depart.Reached() {
		t.Fatal("les deux écritures ne se sont jamais croisées : le rejeu n'est pas éprouvé")
	}

	relu, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, compte := range []string{"prof", "ecote", "jlpicard"} {
		if _, connu := relu.Set.Find(compte); !connu {
			t.Errorf("@%s a disparu : une écriture en a effacé une autre", compte)
		}
	}
}

// Le registre porte des noms d'étudiants. S'il a été rendu public, rien n'y est
// écrit : mieux vaut une écriture qui échoue bruyamment qu'une liste de noms
// exposée sans que personne s'en aperçoive.
func TestRienNEstEcritDansUnRegistrePublic(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, false)
	store, serveur := magasin(t, state)

	_, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote")))
	if err == nil {
		t.Fatal("écrire dans un registre public doit être refusé")
	}
	if !strings.Contains(err.Error(), "public") {
		t.Errorf("le refus doit dire pourquoi : %v", err)
	}
	if commits := serveur.State.CallCount("POST /repos/acme/.cohorte/git/commits"); commits != 0 {
		t.Errorf("%d commit(s) écrit(s) malgré le refus", commits)
	}
}

// Une organisation qui accorde d'office un droit de lecture à ses membres doit
// être signalée : des étudiants membres y liraient la liste de leurs camarades.
func TestPermissionDeBaseSignalee(t *testing.T) {
	state := fakegh.NewState()
	state.DefaultRepoPermission["acme"] = "read"
	store, _ := magasin(t, state)
	avertissement := store.Exposure()
	if !strings.Contains(avertissement, "read") || !strings.Contains(avertissement, registry.RepoName) {
		t.Fatalf("avertissement = %q", avertissement)
	}

	// « none » n'expose rien, et un réglage invisible ne dit rien non plus.
	state.DefaultRepoPermission["acme"] = "none"
	if avertissement := store.Exposure(); avertissement != "" {
		t.Errorf("« none » ne doit rien signaler : %q", avertissement)
	}
	delete(state.DefaultRepoPermission, "acme")
	if avertissement := store.Exposure(); avertissement != "" {
		t.Errorf("un réglage invisible ne permet rien d'affirmer : %q", avertissement)
	}
}

// clientVers monte un second client sur le même faux GitHub.
func clientVers(t *testing.T, serveur *fakegh.Server) *ghapi.Client {
	t.Helper()
	client, err := ghapi.New(ghapi.Options{
		Host: "127.0.0.1", Token: "jeton-de-test", BaseURL: serveur.URL(),
		Sleep: func(time.Duration) {},
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	return client
}

// Le commit relevé scelle ce qu'on a lu : tant qu'il n'a pas changé, le fichier
// n'est pas retéléchargé. Une lecture courante coûte une seule requête.
func TestUneLectureInchangeeNeReteleschargeRien(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	lectures := state.CallCount("GET /repos/acme/.cohorte/contents/")

	for range 3 {
		snapshot, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Set.Name("ecote") != "Émilie Côté" || snapshot.Stale {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	}
	if apres := state.CallCount("GET /repos/acme/.cohorte/contents/"); apres != lectures {
		t.Errorf("%d relecture(s) du fichier alors que le commit n'a pas bougé", apres-lectures)
	}
}

// Un écran demande les noms plusieurs fois : la tête de la branche n'est
// relevée qu'une fois par minute, et ce qu'on vient d'écrire vaut lecture.
func TestLaTeteNEstReleveeQuUneFoisParMinute(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	const tete = "GET /repos/acme/.cohorte/git/ref/heads/main"
	releves := state.CallCount(tete)

	for range 3 {
		snapshot, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Set.Name("ecote") != "Émilie Côté" {
			t.Fatalf("snapshot = %+v", snapshot)
		}
	}
	if apres := state.CallCount(tete); apres != releves {
		t.Errorf("%d relevé(s) de la tête juste après l'avoir écrite", apres-releves)
	}

	store.Avancer(registry.Recheck)
	for range 3 {
		if _, err := store.Load(); err != nil {
			t.Fatal(err)
		}
	}
	if apres := state.CallCount(tete); apres != releves+1 {
		t.Errorf("%d relevé(s) de la tête une minute plus tard, attendu 1", apres-releves)
	}
}

// Sans cache, le registre se relit à chaque fois : c'est ce qu'on a demandé.
func TestSansCacheLaTeteSeReleveAChaqueLecture(t *testing.T) {
	store, serveur := magasin(t, nil)
	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	const tete = "GET /repos/acme/.cohorte/git/ref/heads/main"
	releves := serveur.State.CallCount(tete)
	for range 2 {
		if _, err := store.Load(); err != nil {
			t.Fatal(err)
		}
	}
	if apres := serveur.State.CallCount(tete); apres != releves+2 {
		t.Errorf("%d relevé(s) de la tête pour deux lectures sans cache", apres-releves)
	}
}

// Le commit change dès que quelqu'un écrit : le fichier est alors relu.
func TestUnCommitDifferentFaitRelireLeFichier(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	// Un collègue écrit, sur son propre magasin et sans notre cache.
	ailleurs := registry.New(clientVers(t, serveur), "acme", nil)
	if _, err := ailleurs.Apply(registry.Learn(personne("Jean-Luc Picard", "jlpicard"))); err != nil {
		t.Fatal(err)
	}

	store.Avancer(registry.Recheck)
	snapshot, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, connu := snapshot.Set.Find("jlpicard"); !connu {
		t.Fatal("le registre n'a pas été relu alors que le commit avait changé")
	}
}

// Hors ligne, le registre déjà lu reste ce qu'on sait de mieux — mais il est
// annoncé comme tel : afficher des noms périmés en silence serait pire que de
// n'en afficher aucun.
func TestHorsLigneLeRegistreConnuSertEtLeDit(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	serveur.Close() // plus de GitHub
	store.Avancer(registry.Recheck)

	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("hors ligne, la lecture doit aboutir : %v", err)
	}
	if !snapshot.Stale {
		t.Error("un registre venu du disque doit s'annoncer comme périmé")
	}
	if snapshot.Set.Name("ecote") != "Émilie Côté" {
		t.Fatalf("registre = %+v", snapshot.Set.All())
	}
	// Écrire, en revanche, n'a aucun sens hors ligne.
	if _, err := store.Apply(registry.Learn(personne("Jean-Luc Picard", "jlpicard"))); err == nil {
		t.Error("une écriture hors ligne doit échouer, pas partir d'un état périmé")
	}
}

// Un refus de GitHub n'est pas une panne de liaison : montrer des noms périmés
// au lieu de dire « votre jeton a expiré » égarerait.
func TestUnJetonRefuseNeSeReplieePasSurLeDisque(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatal(err)
	}
	state.FailOn["GET /repos/acme/.cohorte/git/ref/heads/main"] = fakegh.Failure{
		Status: 401, Message: "Bad credentials"}
	store.Avancer(registry.Recheck)

	if _, err := store.Load(); err == nil {
		t.Fatal("un jeton refusé doit remonter, pas se replier sur le disque")
	}
}

// L'historique du registre se réécrit en un commit sans passé : c'est ce qu'on
// peut promettre de mieux à qui demande qu'un nom disparaisse.
func TestLHistoireDuRegistreSeReecrit(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	local := cache.NewIn(t.TempDir(), true)
	store := registry.New(clientVers(t, serveur), "acme", local)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(registry.Learn(personne("Jean-Luc Picard", "jlpicard"))); err != nil {
		t.Fatal(err)
	}
	avant, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}

	orphelin, err := store.ForgetHistory()
	if err != nil {
		t.Fatalf("ForgetHistory : %v", err)
	}
	if orphelin == avant.Head {
		t.Fatal("la branche n'a pas bougé")
	}

	// Le contenu est intact ; c'est le passé qui a disparu.
	apres, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if apres.Set.Len() != 2 || apres.Set.Name("ecote") != "Émilie Côté" {
		t.Fatalf("registre = %+v", apres.Set.All())
	}
	if apres.Head != orphelin {
		t.Fatalf("tête = %q, attendu %q", apres.Head, orphelin)
	}

	// Et l'on peut continuer à écrire par-dessus, comme avant.
	suite, err := store.Apply(registry.Learn(personne("Aminata Diallo", "aminata-d")))
	if err != nil {
		t.Fatalf("écriture après effacement : %v", err)
	}
	if suite.Len() != 3 {
		t.Fatalf("registre = %+v", suite.All())
	}
}

// Sur un registre jamais écrit, il n'y a rien à effacer, et le dire vaut mieux
// que de faire semblant.
func TestEffacerUnHistoriqueInexistantLeDit(t *testing.T) {
	store, _ := magasin(t, nil)
	if _, err := store.ForgetHistory(); err == nil {
		t.Fatal("effacer un historique inexistant doit se signaler")
	}
}

// Un « .cohorte » créé mais jamais rempli — une écriture interrompue, ou un
// dépôt fait à la main sur github.com — ne doit bloquer ni la lecture ni la
// suite. C'est le piège du dépôt vide : GitHub y répond 409 « Git Repository is
// empty. » là où l'on attendrait 404, et un 409 non prévu remonte jusqu'à
// l'écran sous la forme « HTTP 409 — Git Repository is empty. ».
func TestUnRegistreExistantMaisVideNeBloqueRien(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true) // créé, aucun commit
	store, _ := magasin(t, state)

	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("lire un registre vide : %v", err)
	}
	if snapshot.Set.Len() != 0 || snapshot.Head != "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	// Et l'on peut y écrire : c'est le premier commit du dépôt.
	set, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote")))
	if err != nil {
		t.Fatalf("écrire dans un registre vide : %v", err)
	}
	if set.Name("ecote") != "Émilie Côté" {
		t.Fatalf("registre = %+v", set.All())
	}
	relu, err := store.Load()
	if err != nil || relu.Set.Name("ecote") != "Émilie Côté" {
		t.Fatalf("relu = %+v, %v", relu, err)
	}
}

// Un dépôt vide ne doit pas non plus faire échouer ce qui l'entoure. Donner
// accès à une équipe l'amorce au passage : c'est le même besoin, un dépôt que
// GitHub tienne pour un dépôt git.
func TestDonnerAccesAmorceUnDepotVide(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	store, _ := magasin(t, state)

	if err := store.Grant("enseignants"); err != nil {
		t.Fatalf("Grant sur un dépôt vide : %v", err)
	}
	if droit := state.TeamRepos["acme/enseignants"]["acme/"+registry.RepoName]; droit == "" {
		t.Fatalf("aucun droit accordé : %+v", state.TeamRepos)
	}
	fichiers := state.Files("acme/"+registry.RepoName, registry.Branch)
	if !strings.Contains(fichiers[registry.ReadmeFile], "gh milou") {
		t.Fatalf("le dépôt n'a pas été amorcé : %v", sortedNoms(fichiers))
	}
}

// Sur un dépôt qui n'existe pas du tout, il n'y a pas d'historique à effacer,
// et le dire en français vaut mieux qu'un code HTTP.
func TestEffacerLHistoriqueDUnDepotAbsentLeDit(t *testing.T) {
	store, _ := magasin(t, nil)
	_, err := store.ForgetHistory()
	if err == nil {
		t.Fatal("il n'y a rien à effacer : il faut le dire")
	}
	if strings.Contains(err.Error(), "409") || strings.Contains(err.Error(), "HTTP") {
		t.Errorf("le refus doit être en français, pas un code HTTP : %v", err)
	}
}

// Une erreur du registre doit dire de quel dépôt et de quelle étape elle vient.
// « HTTP 409 — Git Repository is empty. » ne disait ni l'un ni l'autre, et cela
// a coûté cher à diagnostiquer.
func TestUneErreurDitCeQuOnFaisait(t *testing.T) {
	state := fakegh.NewState()
	state.FailOn["GET /repos/acme/.cohorte/git/ref/heads/main"] = fakegh.Failure{
		Status: 500, Message: "Panne"}
	store, _ := magasin(t, state)

	_, err := store.Load()
	if err == nil {
		t.Fatal("la lecture devait échouer")
	}
	for _, attendu := range []string{"lecture de la branche", "acme/" + registry.RepoName} {
		if !strings.Contains(err.Error(), attendu) {
			t.Errorf("l'erreur ne dit pas « %s » : %v", attendu, err)
		}
	}
	// Le statut et le type d'origine restent atteignables en aval : c'est ce
	// qui permet encore de proposer un renouvellement de jeton.
	if ghapi.StatusOf(err) != 500 {
		t.Errorf("statut perdu : %d", ghapi.StatusOf(err))
	}
}

// Le refus d'avance rapide, lui, n'est pas enrobé : la boucle d'écriture doit
// continuer à le reconnaître pour rejouer.
func TestLeRefusDAvanceRapideResteReconnaissable(t *testing.T) {
	state := fakegh.NewState()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	store := registry.New(clientVers(t, serveur), "acme", nil)

	if _, err := store.Apply(registry.Learn(personne("Prof Une", "prof"))); err != nil {
		t.Fatal(err)
	}
	// La mise à jour de la référence est refusée à chaque tentative : la boucle
	// doit s'épuiser en le disant, non remonter un HTTP brut.
	state.FailOn["PATCH /repos/acme/.cohorte/git/refs/heads/main"] = fakegh.Failure{
		Status: 422, Message: "Update is not a fast forward"}

	_, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote")))
	if err == nil {
		t.Fatal("l'écriture devait échouer")
	}
	if !strings.Contains(err.Error(), "tentatives d'écriture refusées") {
		t.Fatalf("la boucle n'a pas reconnu le refus : %v", err)
	}
}

// Un dépôt qui a des commits mais pas encore de registre mérite son explication
// autant qu'un dépôt neuf : la condition porte sur le fichier, non sur la tête.
func TestUnDepotDejaGarniRecoitAussiSonExplication(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName,
		map[string]string{"NOTES.md": "posé à la main\n"}, registry.Branch)
	store, _ := magasin(t, state)

	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	fichiers := state.Files("acme/"+registry.RepoName, registry.Branch)
	if _, present := fichiers[registry.ReadmeFile]; !present {
		t.Fatalf("le fichier d'explication manque : %v", sortedNoms(fichiers))
	}
	// Et ce qui était là n'a pas été effacé.
	if fichiers["NOTES.md"] == "" {
		t.Fatalf("un fichier posé à la main a disparu : %v", sortedNoms(fichiers))
	}
}

// L'explication n'est écrite qu'une fois : la réécrire à chaque écriture ferait
// du bruit dans l'historique.
func TestLExplicationNEstEcriteQuUneFois(t *testing.T) {
	state := fakegh.NewState()
	store, _ := magasin(t, state)
	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatal(err)
	}
	blobs := state.CallCount("POST /repos/acme/.cohorte/git/blobs")
	if _, err := store.Apply(registry.Learn(personne("Jean-Luc Picard", "jlpicard"))); err != nil {
		t.Fatal(err)
	}
	// Une écriture suivante ne dépose qu'un blob : le registre, pas l'explication.
	if apres := state.CallCount("POST /repos/acme/.cohorte/git/blobs"); apres-blobs != 1 {
		t.Errorf("%d blob(s) déposé(s) pour une écriture ordinaire", apres-blobs)
	}
}

// sortedNoms rend les noms de fichiers, triés, pour un message d'échec lisible.
func sortedNoms(fichiers map[string]string) []string {
	noms := make([]string, 0, len(fichiers))
	for nom := range fichiers {
		noms = append(noms, nom)
	}
	sort.Strings(noms)
	return noms
}

// L'organisation n'a pas de « .cohorte » : la première publication doit le
// créer, et le créer déjà garni.
//
// Un dépôt sans aucun commit n'est pas un dépôt git pour GitHub, qui répond
// « Git Repository is empty. » à qui vient y lire. Le faire naître avec son
// premier commit évite cet état transitoire au lieu d'avoir à le traverser :
// aucune requête ne part donc vers un dépôt vide.
func TestLeDepotEstCreeDejaGarni(t *testing.T) {
	verifierAucuneRequeteVersUnDepotVide(t, nil)
}

// Et le cas qui a mordu : le dépôt existe déjà, créé par une tentative
// précédente, mais n'a jamais reçu de commit. « auto_init » ne s'applique qu'à
// la création : ce dépôt-là doit être amorcé autrement.
func TestUnDepotDejaCreeMaisVideSAmorce(t *testing.T) {
	verifierAucuneRequeteVersUnDepotVide(t, func(state *fakegh.State) {
		state.AddRepo("acme", registry.RepoName, true) // créé, aucun commit
	})
}

// verifierAucuneRequeteVersUnDepotVide observe chaque requête adressée au
// registre : aucune ne doit partir alors que le dépôt existe sans porter de
// commit, car c'est là que GitHub répond « Git Repository is empty. ».
func verifierAucuneRequeteVersUnDepotVide(t *testing.T, preparer func(*fakegh.State)) {
	t.Helper()
	state := fakegh.NewState()
	if preparer != nil {
		preparer(state)
	}
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)

	depot := "acme/" + registry.RepoName
	var videAuPassage []string
	var mutex sync.Mutex
	state.Hook = func(request *http.Request) {
		if !strings.Contains(request.URL.Path, "/"+registry.RepoName+"/") {
			return
		}
		if state.HasCommits(depot) {
			return
		}
		if _, existe := state.Repos[depot]; !existe {
			return // le dépôt n'est pas encore né : c'est autre chose
		}
		// Deux requêtes ont le droit d'atteindre un dépôt vide, et elles
		// seules. Relever la tête de la branche, dont c'est le travail même
		// que d'apprendre qu'il n'y en a pas — GitHub y répond 409, et le
		// client s'y attend. Et l'API des contenus, la seule qui sache écrire
		// dans un dépôt que l'API Git refuse encore : c'est par elle que
		// l'amorçage passe.
		if strings.HasSuffix(request.URL.Path, "/git/ref/heads/"+registry.Branch) {
			return
		}
		if request.Method == http.MethodPut && strings.Contains(request.URL.Path, "/contents/") {
			return
		}
		mutex.Lock()
		videAuPassage = append(videAuPassage, request.Method+" "+request.URL.Path)
		mutex.Unlock()
	}

	store := registry.New(clientVers(t, serveur), "acme", nil)
	if _, err := store.Apply(registry.Learn(personne("Émilie Côté", "ecote"))); err != nil {
		t.Fatalf("première publication : %v", err)
	}

	if len(videAuPassage) > 0 {
		t.Fatalf("requêtes vers un dépôt sans commit : %v", videAuPassage)
	}
	cree := state.Repos[depot]
	if cree == nil || !cree.Private {
		t.Fatalf("dépôt = %+v", cree)
	}

	// Ce qui explique le dépôt a pris la place du fichier que « auto_init »
	// y avait déposé, plutôt que de s'ajouter à côté.
	fichiers := state.Files(depot, registry.Branch)
	if !strings.Contains(fichiers[registry.ReadmeFile], "gh milou") {
		t.Fatalf("%s = %q", registry.ReadmeFile, fichiers[registry.ReadmeFile])
	}
	if !strings.Contains(fichiers[registry.UsersFile], "Émilie Côté") {
		t.Fatalf("registre = %q", fichiers[registry.UsersFile])
	}
	if len(sortedNoms(fichiers)) != 2 {
		t.Fatalf("le dépôt porte autre chose que ses deux fichiers : %v", sortedNoms(fichiers))
	}
}

// cacheEt monte un magasin doté d'un cache sur disque, et rend les deux.
func cacheEt(t *testing.T, state *fakegh.State) (*registry.Store, *cache.Cache) {
	t.Helper()
	serveur := fakegh.New(state)
	t.Cleanup(serveur.Close)
	stockage := cache.NewIn(filepath.Join(t.TempDir(), "cache"), true)
	return registry.New(clientDe(t, serveur), "acme", stockage), stockage
}

// registreSeme dépose un registre d'une fiche et rend le commit qui le scelle.
func registreSeme(state *fakegh.State) string {
	state.AddRepo("acme", registry.RepoName, true)
	return state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version":2,"users":[{"username":"1680229",` +
			`"full_name":"Prénom Nom","slugs":["prenom-nom"]}]}`,
	}, registry.Branch)
}

// Une entrée de cache écrite par une version antérieure ne se relit pas.
//
// Elle se relisait en silence, ses champs inconnus à zéro : un registre vide
// scellé par le bon commit, donc tenu pour à jour. Les noms que le fichier
// local ne redit plus disparaissaient alors de l'écran, et leurs dépôts avec
// eux — c'est le nom slugifié qui les rattache.
func TestUnCacheDUneFormeInconnueEstIgnore(t *testing.T) {
	state := fakegh.NewState()
	tete := registreSeme(state)
	store, stockage := cacheEt(t, state)

	// Ce qu'une version d'avant le renommage de « students » avait laissé.
	stockage.Set(cache.RegistryKey("acme"), map[string]any{
		"head": tete,
		"students": []map[string]any{
			{"username": "1680229", "full_name": "Prénom Nom"},
		},
	})

	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if nom := snapshot.Set.Name("1680229"); nom != "Prénom Nom" {
		t.Errorf("Name = %q : le cache périmé s'est fait passer pour le registre", nom)
	}
}

// Le cache de la forme courante, lui, épargne bien la lecture du fichier : une
// lecture qui n'a pas bougé ne doit coûter qu'une requête.
func TestUnCacheDeLaFormeCouranteEviteDeRelireLeFichier(t *testing.T) {
	state := fakegh.NewState()
	registreSeme(state)
	store, _ := cacheEt(t, state)

	if _, err := store.Load(); err != nil {
		t.Fatalf("Load : %v", err)
	}
	lectures := state.CallCount("/contents/" + registry.UsersFile)
	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if nom := snapshot.Set.Name("1680229"); nom != "Prénom Nom" {
		t.Errorf("Name = %q depuis le cache", nom)
	}
	if apres := state.CallCount("/contents/" + registry.UsersFile); apres != lectures {
		t.Errorf("%d lecture(s) du fichier, %d avant : le cache ne sert plus", apres, lectures)
	}
}

// Ce que la lecture a dû signaler se redit à chaque fois, cache ou non.
//
// Un registre écrit par une version ultérieure se lit au mieux en partie, au
// pire pas du tout : le renommage de « students » en « users » a rendu, aux
// versions d'avant, une organisation entière sans un seul nom. L'outil le
// disait — une fois. La lecture suivante venait du cache, qui ne retenait que
// les fiches, et l'avertissement se taisait pour la durée du sceau : trois
// mois d'une organisation qui paraît vide sans que rien ne l'explique.
func TestUnAvertissementDuRegistreSurvitAuCache(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		// Une version que cet outil ne connaît pas, et des fiches rangées là
		// où il ne les cherche pas : c'est exactement ce qu'il a fait subir à
		// ses propres versions antérieures.
		registry.UsersFile: `{"version":99,"personnes":[{"username":"1680229"}]}`,
	}, registry.Branch)
	store, _ := cacheEt(t, state)

	premiere, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(premiere.Issues) == 0 {
		t.Fatalf("première lecture : aucun avis sur une version inconnue")
	}
	if premiere.Set.Len() != 0 {
		t.Fatalf("Len = %d : le fichier n'est pourtant pas lisible ici", premiere.Set.Len())
	}

	seconde, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if len(seconde.Issues) != len(premiere.Issues) {
		t.Fatalf("seconde lecture : %d avis contre %d à la première — le cache les a perdus",
			len(seconde.Issues), len(premiere.Issues))
	}
	if !strings.Contains(strings.Join(seconde.Issues, " "), "version 99") {
		t.Errorf("avis relus : %v", seconde.Issues)
	}
}
