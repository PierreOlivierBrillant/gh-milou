package registry_test

import (
	"path/filepath"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/cache"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

// jeton tire une marque, sous sa forme lisible.
func jeton(t *testing.T) string {
	t.Helper()
	tirage, err := signature.New()
	if err != nil {
		t.Fatalf("tirage : %v", err)
	}
	return signature.Text(tirage)
}

// Une marque délivrée par matricule se nomme par lui tant que personne n'a de
// compte, puis par la personne dès que le registre apprend un compte qui le
// porte : c'est ce qui la garde attribuable une fois un compte rattaché. La
// ligne, elle, ne bouge pas — elle dit ce qui désignait la personne le jour où
// la marque lui a été délivrée.
func TestUneMarqueDelivreeParMatriculeResteAttribuable(t *testing.T) {
	sienne := jeton(t)
	set, _, err := appliquer(t, registry.Empty(), registry.Mark(signature.Issued{
		Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: sienne,
	}))
	if err != nil {
		t.Fatalf("versement : %v", err)
	}

	titulaire, connu := set.Holder(sienne)
	if !connu || titulaire.StudentID != "2100456" || titulaire.Username != "" {
		t.Fatalf("titulaire : %+v", titulaire)
	}
	if phrase := titulaire.Phrase(); phrase !=
		"délivrée au matricule 2100456 pour « a26.5n6.01.tp1 »" {
		t.Fatalf("phrase : %q", phrase)
	}

	set, _, err = appliquer(t, set, registry.Learn(roster.Person{
		FullName: "Naomi Chéry", Username: "nchery", StudentID: "2100456",
	}))
	if err != nil {
		t.Fatalf("apprentissage : %v", err)
	}
	titulaire, _ = set.Holder(sienne)
	if titulaire.Username != "nchery" || titulaire.FullName != "Naomi Chéry" {
		t.Fatalf("une fois son compte connu, la marque doit la nommer : %+v", titulaire)
	}
	if phrase := titulaire.Phrase(); phrase !=
		"délivrée à Naomi Chéry pour « a26.5n6.01.tp1 »" {
		t.Fatalf("phrase : %q", phrase)
	}
	if ligne, _ := set.Marks().Who(sienne); ligne.Username != "" || ligne.StudentID != "2100456" {
		t.Fatalf("la ligne a été réécrite : %+v", ligne)
	}
}

// Une marque délivrée à un compte se nomme comme avant : par le nom que le
// registre connaît, et par le compte à défaut. Celle que le registre n'a pas
// délivrée ne désigne personne.
func TestUneMarqueDelivreeAUnCompteSeNommeCommeAvant(t *testing.T) {
	sienne := jeton(t)
	set, _, err := appliquer(t, registry.Empty(), registry.Mark(signature.Issued{
		Assignment: "a26.5n6.01.tp1", Username: "jlpicard", Token: sienne,
	}))
	if err != nil {
		t.Fatalf("versement : %v", err)
	}
	if titulaire, _ := set.Holder(sienne); titulaire.Phrase() !=
		"délivrée à @jlpicard pour « a26.5n6.01.tp1 »" {
		t.Fatalf("sans nom : %+v", titulaire)
	}
	set, _, err = appliquer(t, set, registry.Learn(personne("Jean-Luc Picard", "jlpicard")))
	if err != nil {
		t.Fatalf("apprentissage : %v", err)
	}
	if titulaire, _ := set.Holder(sienne); titulaire.Phrase() !=
		"délivrée à Jean-Luc Picard pour « a26.5n6.01.tp1 »" {
		t.Fatalf("avec son nom : %+v", titulaire)
	}
	if _, connu := set.Holder(jeton(t)); connu {
		t.Fatal("une marque jamais délivrée ne doit désigner personne")
	}
	if _, connu := (*registry.Set)(nil).Holder(sienne); connu {
		t.Fatal("un registre absent ne connaît aucune marque")
	}
}

// Ce qu'un poste a versé, un autre le relit — par le fichier d'abord, par son
// cache ensuite. Une ligne sans compte ne s'y perd ni à l'un ni à l'autre.
func TestUneMarqueSansCompteSeRelitDUnPosteALAutre(t *testing.T) {
	store, serveur := magasin(t, nil)
	sienne := jeton(t)
	if _, err := store.Apply(registry.Mark(signature.Issued{
		Assignment: "a26.5n6.01.tp1", StudentID: "2100456", Token: sienne,
		IssuedAt: "2026-09-01T10:00:00Z",
	})); err != nil {
		t.Fatalf("Apply : %v", err)
	}

	// Deux lancements de l'outil sur un même autre poste : le premier lit le
	// fichier et le scelle sur le disque, le second relit ce sceau.
	dossier := filepath.Join(t.TempDir(), "cache")
	lectures := -1
	for _, lecture := range []string{"depuis le fichier", "depuis le cache"} {
		autre := registry.New(clientDe(t, serveur), "acme", cache.NewIn(dossier, true))
		snapshot, err := autre.Load()
		if err != nil {
			t.Fatalf("Load %s : %v", lecture, err)
		}
		if len(snapshot.Issues) != 0 {
			t.Fatalf("lecture %s : %v", lecture, snapshot.Issues)
		}
		titulaire, connu := snapshot.Set.Holder(sienne)
		if !connu || titulaire.StudentID != "2100456" ||
			titulaire.Issued.IssuedAt != "2026-09-01T10:00:00Z" {
			t.Fatalf("lecture %s : %+v", lecture, titulaire)
		}
		apres := serveur.State.CallCount("/contents/" + registry.MarksFile)
		if lecture == "depuis le cache" && apres != lectures {
			t.Fatalf("le fichier des marques a été relu : la seconde lecture devait " +
				"venir du cache")
		}
		lectures = apres
	}
}

// Une entrée de cache d'avant les marques par matricule ne se relit pas. La
// version qui l'a écrite écartait ces lignes à la lecture : scellée par le bon
// commit, l'entrée les tairait pour toute la durée du sceau.
func TestUnCacheDAvantLesMarquesParMatriculeEstIgnore(t *testing.T) {
	state := fakegh.NewState()
	sienne := jeton(t)
	state.AddRepo("acme", registry.RepoName, true)
	tete := state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version": 2, "users": []}`,
		registry.MarksFile: `{"version": 1, "issued": [{"assignment": "a26.5n6.01.tp1", ` +
			`"student_id": "2100456", "token": "` + sienne + `"}]}`,
	}, registry.Branch)
	store, stockage := cacheEt(t, state)

	// Ce que la version précédente scellait pour ce même commit : la ligne
	// sans compte écartée, le reste intact.
	stockage.Set(cache.RegistryKey("acme"), map[string]any{
		"schema": 8, "head": tete, "users": []any{},
		"marks":  map[string]any{"version": 1, "issued": []any{}},
		"issues": []string{"Signatures : une marque de « a26.5n6.01.tp1 » ne dit pas à qui elle est."},
	})

	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if _, connu := snapshot.Set.Holder(sienne); !connu || len(snapshot.Issues) != 0 {
		t.Fatalf("l'entrée d'une version antérieure a tu la marque délivrée par "+
			"matricule (avis : %v)", snapshot.Issues)
	}
}

// Une marque délivrée à une équipe se nomme par elle, au terminal comme au
// navigateur. Le registre ne dit pas qui la composait : les équipes vivent sur
// GitHub, et leur composition change.
func TestUneMarqueDEquipeSeNommeParSonEquipe(t *testing.T) {
	sienne := jeton(t)
	set, _, err := appliquer(t, registry.Empty(), registry.Mark(signature.Issued{
		Assignment: "a26.5n6.01.projet", Team: "eq1", Token: sienne,
	}))
	if err != nil {
		t.Fatalf("versement : %v", err)
	}
	titulaire, connu := set.Holder(sienne)
	if !connu || titulaire.Team != "eq1" || titulaire.Username != "" ||
		titulaire.StudentID != "" {
		t.Fatalf("titulaire : %+v", titulaire)
	}
	if phrase := titulaire.Phrase(); phrase !=
		"délivrée à l'équipe eq1 pour « a26.5n6.01.projet »" {
		t.Fatalf("phrase : %q", phrase)
	}
}

// Une entrée de cache d'avant les marques d'équipe ne se relit pas : la version
// qui l'a écrite écartait ces lignes à la lecture, et l'entrée, scellée par le
// bon commit, les tairait pour toute la durée du sceau.
func TestUnCacheDAvantLesMarquesDEquipeEstIgnore(t *testing.T) {
	state := fakegh.NewState()
	sienne := jeton(t)
	state.AddRepo("acme", registry.RepoName, true)
	tete := state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version": 2, "users": []}`,
		registry.MarksFile: `{"version": 1, "issued": [{"assignment": "a26.5n6.01.projet", ` +
			`"team": "eq1", "token": "` + sienne + `"}]}`,
	}, registry.Branch)
	store, stockage := cacheEt(t, state)

	stockage.Set(cache.RegistryKey("acme"), map[string]any{
		"schema": 9, "head": tete, "users": []any{},
		"marks":  map[string]any{"version": 1, "issued": []any{}},
		"issues": []string{"Signatures : une marque de « a26.5n6.01.projet » ne dit pas à qui elle est."},
	})

	snapshot, err := store.Load()
	if err != nil {
		t.Fatalf("Load : %v", err)
	}
	if _, connu := snapshot.Set.Holder(sienne); !connu || len(snapshot.Issues) != 0 {
		t.Fatalf("l'entrée d'une version antérieure a tu la marque de l'équipe "+
			"(avis : %v)", snapshot.Issues)
	}
}
