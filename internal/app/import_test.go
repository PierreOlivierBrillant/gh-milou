package app_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// classroomOrg monte une organisation telle que GitHub Classroom la laisse :
// des dépôts « travail-compte », et rien qui dise à qui ils appartiennent.
func classroomOrg(t *testing.T) *fakegh.State {
	t.Helper()
	state := fakegh.NewState()
	for _, nom := range []string{
		"tp1-laubelachance", "tp1-felixboisvert", "tp1-loiselle",
		"projet-final-laubelachance", "projet-final-loiselle",
	} {
		state.AddRepo("acme", nom, true)
	}
	return state
}

// listeOmnivox écrit une liste telle qu'Omnivox l'exporte : Windows-1252, CRLF,
// champs blindés, et pas un seul compte GitHub.
func listeOmnivox(t *testing.T, dossier string, lignes ...string) string {
	t.Helper()
	entete := "No étudiant;Groupe;Nom de l'étudiant;Prénom de l'étudiant;Code perm.;"
	var octets []byte
	for _, ligne := range append([]string{entete}, lignes...) {
		for _, r := range ligne {
			octets = append(octets, byte(r))
		}
		octets = append(octets, '\r', '\n')
	}
	chemin := filepath.Join(dossier, "ListeEtudiants.csv")
	if err := os.WriteFile(chemin, octets, 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

func liste(t *testing.T) string {
	return listeOmnivox(t, t.TempDir(),
		`="1600105";="1030";="Aube-Lachance";="Lorenzo";="AUBL10109901";`,
		`="2100106";="1030";="Boisvert";="Félix";="BOIF10109902";`,
		`="1900107";="1030";="Loiselle";="Édouard";="LOIE10109903";`,
	)
}

// Le parcours complet : des dépôts de GitHub Classroom, une liste d'Omnivox
// sans comptes, et un travail qui entre dans la nomenclature.
func TestImportDepuisGitHubClassroom(t *testing.T) {
	state := classroomOrg(t)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	for _, attendu := range []string{
		"a26.5n6.1030.tp1.lorenzo-aube-lachance",
		"a26.5n6.1030.tp1.felix-boisvert",
		"a26.5n6.1030.tp1.edouard-loiselle",
	} {
		if !slices.Contains(noms, attendu) {
			t.Fatalf("« %s » manque : %v", attendu, noms)
		}
	}
	// L'autre travail n'a pas bougé : on n'importe que ce qu'on a demandé.
	if !slices.Contains(noms, "projet-final-loiselle") {
		t.Fatalf("un travail non demandé a bougé : %v", noms)
	}

	// Les noms sont montés au registre : c'est là qu'ils vivent désormais.
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	for _, attendu := range []string{"Lorenzo Aube-Lachance", "laubelachance", "Édouard Loiselle"} {
		if !strings.Contains(contenu, attendu) {
			t.Fatalf("« %s » manque au registre :\n%s", attendu, contenu)
		}
	}
}

// La simulation montre tout et n'écrit rien.
func TestImportEnSimulationNEcritRien(t *testing.T) {
	state := classroomOrg(t)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.DryRun = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	// Le rapprochement est montré, avec la raison qui l'a produit.
	h.contient("Lorenzo Aube-Lachance", "→ a26.5n6.1030.tp1.", "Simulation")
	if noms := h.depots(); !slices.Contains(noms, "tp1-laubelachance") {
		t.Fatalf("un dépôt a été renommé en simulation : %v", noms)
	}
}

// Sans travail nommé, le mode script refuse plutôt que de deviner — mais il a
// d'abord montré ce que les dépôts dessinent.
func TestImportSansTravailListeLesCandidats(t *testing.T) {
	h := nouveau(t, classroomOrg(t))
	h.Options.ImportRequested = true
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("tp1", "projet-final", "--import")
}

// Le travail peut prendre un autre nom au passage.
func TestImportRenommeLeTravailAuPassage(t *testing.T) {
	state := classroomOrg(t)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "projet-final"
	h.Options.RenameTo = "Projet de session"
	h.Options.Into = "h27.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if noms := h.depots(); !slices.Contains(noms,
		"h27.5n6.1030.projet-de-session.edouard-loiselle") {
		t.Fatalf("dépôts = %v", noms)
	}
}

// Une ressemblance devinée se tranche à la main, avant que rien ne soit écrit.
// C'est le même jugement que l'interface web rend au même moment ; sans lui, le
// terminal ne saurait que tout accepter ou tout refuser.
func TestImportCorrigeUnRapprochementALaMain(t *testing.T) {
	state := classroomOrg(t)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = listeOmnivox(t, t.TempDir(),
		`="1600105";="1030";="Aube-Lachance";="Lorenzo";="AUBL10109901";`,
		`="2100106";="1030";="Boisvert";="Félix";="BOIF10109902";`,
		`="1900107";="1030";="Loiselle";="Édouard";="LOIE10109903";`,
		`="1990022";="1030";="Tremblay";="Sophie";="TRES33040506";`,
	)

	code, _ := h.script(
		"tous",            // reprendre tous les dépôts du travail
		"oui",             // corriger un rapprochement
		"loiselle",        // le compte à reprendre
		"Sophie Tremblay", // la personne qu'il désigne vraiment
		"non",             // rien d'autre à corriger
		"oui",             // renommer
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	if !slices.Contains(noms, "a26.5n6.1030.tp1.sophie-tremblay") {
		t.Fatalf("la correction n'a pas été suivie : %v", noms)
	}
	if slices.Contains(noms, "a26.5n6.1030.tp1.edouard-loiselle") {
		t.Fatalf("le rapprochement deviné a été gardé : %v", noms)
	}

	// La personne libérée ne reste associée à personne : la proposer encore
	// ferait croire qu'elle a un dépôt.
	menu, trouve := h.dernierMenu("Étudiant pour @loiselle")
	if !trouve {
		t.Fatalf("aucun menu de correction :\n%s", h.texte())
	}
	for _, option := range menu.Options {
		if option.Label == "Félix Boisvert" {
			t.Fatalf("une personne déjà prise a été proposée : %+v", menu.Options)
		}
	}
}

// La place d'arrivée arrive proposée : le cours vient du nom du fichier, le
// groupe de sa colonne, la session du premier commit du travail.
func TestImportProposeLaPlaceDArrivee(t *testing.T) {
	state := classroomOrg(t)
	// Un travail donné en septembre : c'est la session d'automne.
	state.Repos["acme/tp1-loiselle"].History = fakegh.Commits("2026-09-14T08:00:00Z")

	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Roster = listeOmnivox(t, t.TempDir(),
		`="1600105";="1040";="Aube-Lachance";="Lorenzo";="AUBL10109901";`,
		`="1900107";="1040";="Loiselle";="Édouard";="LOIE10109903";`,
	)
	// Le nom du fichier porte le cours et le groupe ; la colonne dit le groupe.
	chemin := filepath.Join(filepath.Dir(h.Options.Roster),
		"ListeEtudiants_cours4203N5EM_gr1040.csv")
	if err := os.Rename(h.Options.Roster, chemin); err != nil {
		t.Fatal(err)
	}
	h.Options.Roster = chemin

	code, scripte := h.script(
		"tous", // reprendre tous les dépôts du travail
		"",     // la place proposée convient
		"non",  // rien à corriger
		"non",  // reprendre aussi le dépôt sans étudiant connu
		"oui",  // renommer
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if len(scripte.Questions) == 0 {
		t.Fatalf("aucune question posée :\n%s", h.texte())
	}
	if !slices.Contains(h.depots(), "a26.3n5.1040.tp1.edouard-loiselle") {
		t.Fatalf("la place proposée n'a pas été suivie : %v", h.depots())
	}
}

// Les dépôts dont personne n'a été reconnu peuvent rester où ils sont : les
// reprendre les ferait entrer dans la nomenclature sous un dernier niveau qui
// n'est pas un nom.
func TestImportPeutLaisserLesDepotsSansEtudiant(t *testing.T) {
	state := classroomOrg(t)
	state.AddRepo("acme", "tp1-visiteur-anonyme-42", true)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)

	code, _ := h.script(
		"tous", // reprendre tous les dépôts du travail
		"non",  // rien à corriger
		"oui",  // laisser où ils sont ceux qu'on ne connaît pas
		"oui",  // renommer
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	if !slices.Contains(noms, "tp1-visiteur-anonyme-42") {
		t.Fatalf("le dépôt inconnu a été repris : %v", noms)
	}
	if !slices.Contains(noms, "a26.5n6.1030.tp1.edouard-loiselle") {
		t.Fatalf("les dépôts connus n'ont pas été repris : %v", noms)
	}
}

// Le même choix se prend au drapeau, sans personne pour répondre.
func TestImportNamedOnlyAuDrapeau(t *testing.T) {
	state := classroomOrg(t)
	state.AddRepo("acme", "tp1-visiteur-anonyme-42", true)
	h := nouveau(t, state)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.NamedOnly = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if !slices.Contains(h.depots(), "tp1-visiteur-anonyme-42") {
		t.Fatalf("le dépôt inconnu a été repris : %v", h.depots())
	}
	h.contient("1 dépôt sans étudiant connu", "laissé où il est")
}

// Un travail ne se reprend pas toujours en entier : on choisit les dépôts, et
// les autres restent où ils sont.
func TestImportNeReprendQueLesDepotsChoisis(t *testing.T) {
	h := nouveau(t, classroomOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)

	code, _ := h.script(
		"1,2", // deux dépôts sur trois
		"non", // rien à corriger
		"oui", // renommer
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	repris := 0
	for _, nom := range noms {
		if strings.HasPrefix(nom, "a26.5n6.1030.tp1.") {
			repris++
		}
	}
	if repris != 2 {
		t.Fatalf("%d dépôt(s) repris, deux attendus : %v", repris, noms)
	}
	if !slices.Contains(noms, "tp1-loiselle") {
		t.Fatalf("le dépôt écarté a été repris quand même : %v", noms)
	}
}

// Le même choix se prend au drapeau, sans personne pour répondre.
func TestImportChoisitLesDepotsAuDrapeau(t *testing.T) {
	h := nouveau(t, classroomOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.Repos = "tp1-laubelachance"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	if !slices.Contains(noms, "a26.5n6.1030.tp1.lorenzo-aube-lachance") {
		t.Fatalf("le dépôt choisi n'a pas été repris : %v", noms)
	}
	for _, reste := range []string{"tp1-felixboisvert", "tp1-loiselle"} {
		if !slices.Contains(noms, reste) {
			t.Fatalf("« %s » a été repris alors qu'il n'était pas choisi : %v", reste, noms)
		}
	}
}

// Un nom tapé de travers n'est pas un choix : il est refusé plutôt qu'ignoré.
func TestImportRefuseUnDepotInconnuAuDrapeau(t *testing.T) {
	h := nouveau(t, classroomOrg(t))
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Roster = liste(t)
	h.Options.Repos = "tp1-personne-de-ce-nom"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("n'est pas un dépôt")
}

// Quand l'organisation nomme déjà tout le monde, la liste n'apprend rien : la
// reprise se fait sans elle.
func TestImportSansListeQuandLesNomsSontConnus(t *testing.T) {
	state := classroomOrg(t)
	h := nouveau(t, state)
	// Un groupe déjà déclaré nomme les comptes de ce travail.
	ancien, err := classroom.AtScope("acme", "a25.5n6.1030",
		classroom.DefaultsFrom(config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	ancien.Students = []roster.Person{
		{FullName: "Lorenzo Aube-Lachance", Username: "laubelachance"},
		{FullName: "Félix Boisvert", Username: "felixboisvert"},
		{FullName: "Édouard Loiselle", Username: "loiselle"},
	}
	h.declarer(ancien)
	h.Options.ImportRequested = true
	h.Options.Import = "tp1"
	h.Options.Into = "a26.5n6.1030"
	h.Options.Yes = true // aucune liste passée

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	noms := h.depots()
	for _, attendu := range []string{
		"a26.5n6.1030.tp1.lorenzo-aube-lachance",
		"a26.5n6.1030.tp1.felix-boisvert",
		"a26.5n6.1030.tp1.edouard-loiselle",
	} {
		if !slices.Contains(noms, attendu) {
			t.Fatalf("« %s » manque : %v", attendu, noms)
		}
	}
}
