package classroom_test

import (
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

func inscrits() []roster.Entry {
	return []roster.Entry{
		{FullName: "Lorenzo Aube-Lachance", StudentID: "1600105"},
		{FullName: "Félix Boisvert", StudentID: "2100106"},
		{FullName: "Édouard Loiselle", StudentID: "1900107"},
	}
}

func arrivee() classroom.Classroom {
	cours, err := classroom.AtScope("acme", "a26.5n6.1030",
		classroom.DefaultsFrom(config.Default()))
	if err != nil {
		panic(err)
	}
	return cours
}

// Ce que l'organisation porte hors nomenclature, et les travaux que ses
// préfixes dessinent.
func TestDepotsHorsNomenclatureEtLeursTravaux(t *testing.T) {
	inventaire := depots(
		"tp1-laubelachance", "tp1-felixb", "tp1-loiselle",
		"projet-final-laubelachance", "projet-final-felixb",
		"a26.5n6.1030.tp2.lorenzo-aube-lachance", // déjà à sa place
	)
	dehors := classroom.ForeignOf(inventaire)
	if len(dehors.Repos) != 5 {
		t.Fatalf("dépôts hors nomenclature : %v", dehors.Repos)
	}
	for _, nom := range dehors.Repos {
		if strings.HasPrefix(nom, "a26.") {
			t.Fatalf("un dépôt déjà à sa place a été compté : %v", dehors.Repos)
		}
	}
	prefixes := map[string]int{}
	for _, travail := range dehors.Assignments {
		prefixes[travail.Prefix] = travail.Count
	}
	if prefixes["tp1"] != 3 || prefixes["projet-final"] != 2 {
		t.Fatalf("travaux devinés : %+v", dehors.Assignments)
	}
}

// Le cas complet : une liste d'Omnivox sans comptes, des dépôts nommés par
// GitHub Classroom, et un travail à faire entrer dans la nomenclature.
func TestImportRapprocheEtRenomme(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixboisvert", "tp1-loiselle")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Name: "tp1", Entries: inscrits(), Guess: true}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Moves) != 3 || len(plan.Unmatched) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-laubelachance"] != "a26.5n6.1030.tp1.lorenzo-aube-lachance" {
		t.Fatalf("cibles = %v", cibles)
	}
	if cibles["tp1-loiselle"] != "a26.5n6.1030.tp1.edouard-loiselle" {
		t.Fatalf("cibles = %v", cibles)
	}
	// Le compte vient du dépôt, le nom de la liste : c'est ce couple que le
	// registre attend.
	if len(plan.Students) != 3 {
		t.Fatalf("personnes inscrites = %+v", plan.Students)
	}
	for _, personne := range plan.Students {
		if personne.Username == "" || personne.FullName == "" {
			t.Fatalf("personne incomplète : %+v", personne)
		}
	}
}

// Le travail peut prendre un autre nom au passage : c'est le seul moment où
// corriger « tp1-final-v2 » ne coûte rien.
func TestImportPeutRenommerLeTravail(t *testing.T) {
	inventaire := depots("projet-final-loiselle")
	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "projet-final", Name: "Projet final", Entries: inscrits(), Guess: true},
		inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Moves) != 1 ||
		plan.Moves[0].Target != "a26.5n6.1030.projet-final.edouard-loiselle" {
		t.Fatalf("plan = %+v", plan.Moves)
	}
}

// Quand la liste porte les comptes, rien n'est deviné.
func TestUneListeQuiDitLesComptesNeSeDevinePas(t *testing.T) {
	inventaire := depots("tp1-xkcd42")
	dits := []roster.Entry{
		{FullName: "Édouard Loiselle", StudentID: "1900107", Username: "xkcd42"},
		{FullName: "Félix Boisvert", StudentID: "2100106"},
	}
	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Name: "tp1", Entries: dits, Guess: true}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Pairings) != 1 || plan.Pairings[0].Entry.FullName != "Édouard Loiselle" {
		t.Fatalf("rapprochements = %+v", plan.Pairings)
	}
	if !strings.Contains(plan.Pairings[0].Reason, "liste") {
		t.Fatalf("raison = %q", plan.Pairings[0].Reason)
	}
	// Félix n'a pas de dépôt pour ce travail : le dire évite de croire la
	// liste entière importée.
	if len(plan.Absent) != 1 || plan.Absent[0] != "Félix Boisvert" {
		t.Fatalf("absents = %v", plan.Absent)
	}
}

// Un dépôt dont le compte ne mène à personne garde ce compte comme dernier
// niveau : on ne peut pas lui inventer un nom, et le laisser derrière ferait un
// travail à moitié importé. Le plan le nomme, pour qu'on puisse le corriger.
func TestUnDepotSansPersonneGardeSonCompte(t *testing.T) {
	inventaire := depots("tp1-loiselle", "tp1-visiteur-anonyme-42")
	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Name: "tp1", Entries: inscrits(), Guess: true}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Unmatched) != 1 || plan.Unmatched[0] != "visiteur-anonyme-42" {
		t.Fatalf("dépôts sans personne = %v", plan.Unmatched)
	}
	// Il figure quand même au renommage, sous le nom qu'il porte : le laisser
	// derrière ferait un travail à moitié importé.
	if len(plan.Moves) != 2 {
		t.Fatalf("renommages = %+v", plan.Moves)
	}
	for _, ligne := range plan.Moves {
		if ligne.Repo == "tp1-visiteur-anonyme-42" &&
			ligne.Target != "a26.5n6.1030.tp1.visiteur-anonyme-42" {
			t.Fatalf("le dépôt inconnu devait garder son dernier niveau : %+v", ligne)
		}
	}
}

// Un préfixe qui ne désigne aucun dépôt se refuse plutôt que de rendre un plan
// vide qu'on prendrait pour un succès.
func TestUnPrefixeSansDepotEstRefuse(t *testing.T) {
	if _, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "absent", Name: "tp1", Entries: inscrits(), Guess: true},
		depots("tp1-loiselle")); err == nil {
		t.Fatal("un préfixe sans dépôt doit être refusé")
	}
}

// Sans rapprochement, les comptes que la liste ne nomme pas restent sans
// réponse : c'est ce qu'on a décidé à l'écran, et redeviner le déferait.
func TestSansRapprochementRienNEstDevine(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-loiselle")
	corrige := []roster.Entry{
		{FullName: "Édouard Loiselle", Username: "tp1-inexistant"},
	}
	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Name: "tp1", Entries: corrige}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Unmatched) != 2 {
		t.Fatalf("des comptes ont été devinés : %+v", plan.Pairings)
	}
	// L'ordre des dépôts est tenu même quand rien n'est reconnu.
	if len(plan.Pairings) != 2 || plan.Pairings[0].Login != "laubelachance" {
		t.Fatalf("rapprochements = %+v", plan.Pairings)
	}
}

// La place d'arrivée se devine : le cours dans le nom du fichier, le groupe
// dans la colonne qui le porte, la session dans le premier commit du travail.
func TestLaPlaceDArriveeSeDevine(t *testing.T) {
	entrees := []roster.Entry{
		{FullName: "Édouard Loiselle", Group: "1040"},
		{FullName: "Mei Chen", Group: "1040"},
	}
	premier := time.Date(2026, time.September, 14, 8, 0, 0, 0, time.UTC)
	place := classroom.GuessPlace("ListeEtudiants_cours4203N5EM_gr1040.csv", entrees, premier)
	if place.Scope() != "a26.3N5.1040" {
		t.Fatalf("place = %+v", place)
	}
}

// Les mois du printemps sont ceux de l'hiver : « p26 » se compose et se lit,
// mais aucune date ne le désigne, alors la devinette ne le propose jamais.
func TestLePrintempsNeSeDevinePas(t *testing.T) {
	saisons := map[time.Month]string{
		time.January: "h26", time.April: "h26", time.May: "h26",
		time.June: "e26", time.July: "e26",
		time.August: "a26", time.December: "a26",
	}
	for mois, attendu := range saisons {
		moment := time.Date(2026, mois, 15, 0, 0, 0, 0, time.UTC)
		if devinee := classroom.GuessPlace("", nil, moment); devinee.Session != attendu {
			t.Fatalf("%s → %q, attendu %q", mois, devinee.Session, attendu)
		}
	}
}

// Ce qui n'a pas pu être deviné reste vide : inventer une place ferait
// renommer des dépôts au mauvais endroit.
func TestCeQuiNeSeDevinePasResteVide(t *testing.T) {
	place := classroom.GuessPlace("cohorte.csv", nil, time.Time{})
	if place.Session != "" || place.Course != "" || place.Group != "" {
		t.Fatalf("place inventée : %+v", place)
	}
	if place.Scope() != "" {
		t.Fatalf("une place à trous a produit une portée : %q", place.Scope())
	}
}

// La recherche de la date s'arrête après quelques dépôts : si les premiers sont
// vides, personne n'a encore rien remis, et interroger tout le groupe ne
// changerait rien qu'au temps d'attente.
func TestLaDateSeChercheDansQuelquesDepotsSeulement(t *testing.T) {
	var noms []string
	inventaire := depots("tp1-a", "tp1-b", "tp1-c", "tp1-d", "tp1-e", "tp1-f", "tp1-g")
	debut := classroom.AssignmentStart("tp1", inventaire, func(depot string) (time.Time, error) {
		noms = append(noms, depot)
		return time.Time{}, nil
	})
	if !debut.IsZero() {
		t.Fatalf("une date est sortie de dépôts vides : %s", debut)
	}
	if len(noms) != 5 {
		t.Fatalf("%d dépôts interrogés : %v", len(noms), noms)
	}
}

// Le premier dépôt qui répond suffit : les suivants ne sont pas dérangés.
func TestLaDateSArreteAuPremierDepotQuiRepond(t *testing.T) {
	var demandes int
	attendu := time.Date(2027, time.February, 3, 10, 0, 0, 0, time.UTC)
	debut := classroom.AssignmentStart("tp1", depots("tp1-a", "tp1-b", "tp1-c"),
		func(string) (time.Time, error) {
			demandes++
			return attendu, nil
		})
	if !debut.Equal(attendu) || demandes != 1 {
		t.Fatalf("début = %s après %d demande(s)", debut, demandes)
	}
}

// On peut ne reprendre que les dépôts dont on connaît la personne : les autres
// restent où ils sont plutôt que d'entrer dans la nomenclature sous un dernier
// niveau qui n'est pas un nom.
func TestOnPeutNeReprendreQueLesDepotsNommes(t *testing.T) {
	inventaire := depots("tp1-loiselle", "tp1-visiteur-anonyme-42")
	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Name: "tp1", Entries: inscrits(), Guess: true,
		NamedOnly: true}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Moves) != 1 ||
		plan.Moves[0].Target != "a26.5n6.1030.tp1.edouard-loiselle" {
		t.Fatalf("renommages = %+v", plan.Moves)
	}
	// Il est quand même nommé : le laisser derrière en silence ferait croire
	// le travail entièrement repris.
	if len(plan.Unmatched) != 1 || plan.Unmatched[0] != "visiteur-anonyme-42" {
		t.Fatalf("dépôts sans personne = %v", plan.Unmatched)
	}
	if !plan.NamedOnly {
		t.Fatal("le plan ne dit pas ce qu'il advient des dépôts sans personne")
	}
}

// Le cas qui a motivé la lecture des accès : « kickmyb-firebase » est le
// travail, « Wasim7Zed » le compte, et rien dans le nom ne dit où couper. Sans
// les accès, l'outil retenait « firebase-Wasim7Zed » comme compte GitHub — un
// compte qui n'existe pas, et qu'il rapprochait ensuite d'un nom au hasard.
func TestImportPrendLeCompteDansLesAcces(t *testing.T) {
	inventaire := depots(
		"kickmyb-firebase-Wasim7Zed", "kickmyb-firebase-felixb", "kickmyb-firebase-loiselle")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		// Le préfixe deviné s'arrête à « kickmyb » : c'est ce que l'écran
		// propose, et ce que les accès vont corriger.
		Prefix: "kickmyb", Name: "kickmyb", Entries: inscrits(), Guess: true,
		Owners: map[string]string{
			"kickmyb-firebase-Wasim7Zed": "Wasim7Zed",
			"kickmyb-firebase-felixb":    "felixb",
			"kickmyb-firebase-loiselle":  "loiselle",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if plan.Divided() {
		t.Fatalf("un seul travail attendu : %+v", plan.Splits)
	}
	// Le travail est celui que les accès révèlent, et le nom d'arrivée le suit.
	if plan.Prefix != "kickmyb-firebase" || plan.Name != "kickmyb-firebase" {
		t.Fatalf("travail = %q, nom = %q", plan.Prefix, plan.Name)
	}
	for _, trouve := range plan.Pairings {
		if strings.Contains(strings.ToLower(trouve.Login), "firebase") {
			t.Fatalf("le compte porte encore le travail : %+v", trouve)
		}
	}
	if len(plan.Unconfirmed) != 0 {
		t.Fatalf("tous les comptes viennent des accès : %v", plan.Unconfirmed)
	}
	for _, ligne := range plan.Moves {
		if !strings.HasPrefix(ligne.Target, "a26.5n6.1030.kickmyb-firebase.") {
			t.Fatalf("cible = %q", ligne.Target)
		}
	}
}

// Deux travaux sous un même préfixe : il n'y a rien à reprendre tel quel, et
// c'est une question, pas une panne.
func TestImportSepareDeuxTravauxDunMemePrefixe(t *testing.T) {
	inventaire := depots(
		"kickmyb-firebase-wasim", "kickmyb-firebase-felixb",
		"kickmyb-android-loiselle")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "kickmyb", Entries: inscrits(), Guess: true,
		Owners: map[string]string{
			"kickmyb-firebase-wasim":   "wasim",
			"kickmyb-firebase-felixb":  "felixb",
			"kickmyb-android-loiselle": "loiselle",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if !plan.Divided() {
		t.Fatalf("deux travaux attendus : %+v", plan.Splits)
	}
	if len(plan.Moves) != 0 {
		t.Fatalf("rien ne doit être écrit tant que le travail n'est pas choisi : %+v", plan.Moves)
	}
	// Le plus fourni d'abord : c'est celui qu'on proposera en premier.
	if plan.Splits[0].Prefix != "kickmyb-firebase" || plan.Splits[0].Count != 2 {
		t.Fatalf("travaux = %+v", plan.Splits)
	}
	if plan.Splits[1].Prefix != "kickmyb-android" || plan.Splits[1].Count != 1 {
		t.Fatalf("travaux = %+v", plan.Splits)
	}

	// Le travail choisi, la reprise redevient ordinaire.
	choisi, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "kickmyb-firebase", Entries: inscrits(), Guess: true,
		Owners: map[string]string{
			"kickmyb-firebase-wasim":  "wasim",
			"kickmyb-firebase-felixb": "felixb",
		},
	}, inventaire)
	if err != nil || choisi.Divided() || len(choisi.Moves) != 2 {
		t.Fatalf("plan = %+v, err = %v", choisi, err)
	}
}

// Un dépôt auquel personne n'a accès se lit par son nom, comme avant — mais on
// le dit, parce que c'est le seul compte qui puisse encore être faux.
func TestImportSignaleLesComptesQueLesAccesNontPasConfirmes(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixb")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		Owners: map[string]string{"tp1-laubelachance": "laubelachance"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Unconfirmed) != 1 || plan.Unconfirmed[0] != "tp1-felixb" {
		t.Fatalf("non confirmés = %v", plan.Unconfirmed)
	}
	if len(plan.Moves) != 2 {
		t.Fatalf("les deux dépôts se reprennent quand même : %+v", plan.Moves)
	}
}

// Un compte que l'organisation sait déjà nommer n'a rien à faire dans un
// rapprochement : la réponse est écrite, et la chercher par ressemblance ne
// ferait que la retrouver moins bien — quand elle la retrouve.
func TestImportAssocieDembleeCeQueLOrganisationConnait(t *testing.T) {
	inventaire := depots("tp1-xy42", "tp1-felixboisvert")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		// « xy42 » ne ressemble à aucun nom de la liste : sans le registre, il
		// resterait sans réponse et demanderait un coup d'œil.
		Known: map[string]string{"xy42": "Édouard Loiselle"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	trouves := map[string]string{}
	raisons := map[string]string{}
	for _, trouve := range plan.Pairings {
		trouves[trouve.Login] = trouve.Entry.FullName
		raisons[trouve.Login] = trouve.Reason
	}
	if trouves["xy42"] != "Édouard Loiselle" {
		t.Fatalf("rapprochements = %+v", trouves)
	}
	if raisons["xy42"] != "déjà connu de l'organisation" {
		t.Fatalf("raison = %q", raisons["xy42"])
	}
	// Le numéro d'étudiant de la liste est conservé : c'est la même personne.
	for _, trouve := range plan.Pairings {
		if trouve.Login == "xy42" && trouve.Entry.StudentID != "1900107" {
			t.Fatalf("l'entrée de la liste devait servir : %+v", trouve.Entry)
		}
	}
	// Et le nom pris ne peut plus être donné à un autre compte.
	if trouves["tp1-felixboisvert"] == "Édouard Loiselle" {
		t.Fatalf("un nom a été attribué deux fois : %+v", trouves)
	}
}

// Une personne que la liste ne contient pas mais que l'organisation connaît
// garde son nom : son dépôt est bien le sien.
func TestImportNommeUnCompteConnuAbsentDeLaListe(t *testing.T) {
	inventaire := depots("tp1-ancienne", "tp1-felixboisvert")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		Known: map[string]string{"ancienne": "Camille Tremblay"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-ancienne"] != "a26.5n6.1030.tp1.camille-tremblay" {
		t.Fatalf("cibles = %v", cibles)
	}
}

// Après une correction à l'écran, plus rien n'est deviné ni repris du registre :
// le jugement rendu tient, y compris quand il consiste à ne rapprocher personne.
func TestImportNeDefaitPasUnChoixManuelAvecLeRegistre(t *testing.T) {
	inventaire := depots("tp1-xy42", "tp1-felixboisvert")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1",
		// La liste renvoyée par l'écran ne rattache personne à « xy42 ».
		Entries: []roster.Entry{{FullName: "Félix Boisvert", Username: "felixboisvert"}},
		Guess:   false,
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	for _, trouve := range plan.Pairings {
		if trouve.Login == "xy42" && trouve.Found() {
			t.Fatalf("un choix « personne » a été défait : %+v", trouve)
		}
	}
}

// Un compte que l'organisation nomme n'est pas un compte douteux, même si rien
// ne donne accès au dépôt : il vient du registre, donc il existe et il désigne
// quelqu'un. Le signaler ferait douter de ce qui est écrit.
func TestImportNeDouteJamaisDunCompteQueLeRegistreNomme(t *testing.T) {
	inventaire := depots("tp1-felixb", "tp1-loiselle", "tp1-inconnu-x")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		// Aucun accès n'est connu — la carte est vide.
		Owners: nil,
		Known: map[string]string{
			"felixb":   "Félix Boisvert",
			"loiselle": "Édouard Loiselle",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Unconfirmed) != 1 || plan.Unconfirmed[0] != "tp1-inconnu-x" {
		t.Fatalf("non confirmés = %v : seul le compte que rien ne connaît doit l'être",
			plan.Unconfirmed)
	}
}

// Le doute reste entier quand le compte ne vient que du nom : c'est le cas qui
// a motivé la lecture des accès, et un nom trouvé par ressemblance ne le lève
// pas.
func TestImportDouteEncoreDunCompteQueSeulLeNomDonne(t *testing.T) {
	inventaire := depots("tp1-felixboisvert")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	// Le rapprochement a bien trouvé quelqu'un…
	if len(plan.Pairings) != 1 || !plan.Pairings[0].Found() {
		t.Fatalf("rapprochements = %+v", plan.Pairings)
	}
	// … et le compte reste pourtant à confirmer : il n'est lu que dans le nom.
	if len(plan.Unconfirmed) != 1 {
		t.Fatalf("non confirmés = %v", plan.Unconfirmed)
	}
}

// Un travail ne se reprend pas toujours en entier : un dépôt d'essai, celui
// d'une personne qui a abandonné. Ce qu'on décoche est écarté pour de bon.
func TestImportNeReprendQueLesDepotsRetenus(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixboisvert", "tp1-essai")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		Only: []string{"tp1-laubelachance", "tp1-felixboisvert"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Moves) != 2 {
		t.Fatalf("renommages = %+v", plan.Moves)
	}
	for _, ligne := range plan.Moves {
		if strings.Contains(ligne.Repo, "essai") {
			t.Fatalf("un dépôt écarté a été repris : %+v", plan.Moves)
		}
	}
	// Il ne se rapproche pas non plus : il n'est plus de la partie.
	for _, trouve := range plan.Pairings {
		if trouve.Login == "essai" {
			t.Fatalf("un dépôt écarté a été rapproché : %+v", trouve)
		}
	}
}

// Une sélection vide n'en est pas une : le travail est repris entier.
func TestImportSansSelectionReprendToutLeTravail(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixboisvert")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true, Only: nil,
	}, inventaire)
	if err != nil || len(plan.Moves) != 2 {
		t.Fatalf("plan = %+v, err = %v", plan, err)
	}
}

// Tout décocher n'écrit rien, et le dit.
func TestImportRefuseUneSelectionQuiNeGardeRien(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixboisvert")

	_, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: inscrits(), Guess: true,
		Only: []string{"tp1-personne-de-ce-nom"},
	}, inventaire)
	if err == nil || !strings.Contains(err.Error(), "Aucun dépôt retenu") {
		t.Fatalf("erreur = %v", err)
	}
}

// Les dépôts écartés ne comptent pas dans les travaux qu'un préfixe cachait :
// ne garder que « kickmyb-firebase » ne doit plus poser de question.
func TestImportSelectionResoutUnPrefixeFourreTout(t *testing.T) {
	inventaire := depots(
		"kickmyb-firebase-wasim", "kickmyb-firebase-felixb", "kickmyb-android-loiselle")
	acces := map[string]string{
		"kickmyb-firebase-wasim":   "wasim",
		"kickmyb-firebase-felixb":  "felixb",
		"kickmyb-android-loiselle": "loiselle",
	}

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "kickmyb", Entries: inscrits(), Guess: true, Owners: acces,
		Only: []string{"kickmyb-firebase-wasim", "kickmyb-firebase-felixb"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if plan.Divided() {
		t.Fatalf("un seul travail reste sélectionné : %+v", plan.Splits)
	}
	if plan.Prefix != "kickmyb-firebase" || len(plan.Moves) != 2 {
		t.Fatalf("plan = %+v", plan)
	}
}

// Quand l'organisation connaît déjà tout le monde, la liste d'Omnivox n'apprend
// rien : la reprise doit pouvoir s'en passer.
func TestImportSansListeQuandLOrganisationConnaitDeja(t *testing.T) {
	inventaire := depots("tp1-laubelachance", "tp1-felixb")

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: nil, Guess: true,
		Known: map[string]string{
			"laubelachance": "Lorenzo Aube-Lachance",
			"felixb":        "Félix Boisvert",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp1-laubelachance"] != "a26.5n6.1030.tp1.lorenzo-aube-lachance" ||
		cibles["tp1-felixb"] != "a26.5n6.1030.tp1.felix-boisvert" {
		t.Fatalf("cibles = %v", cibles)
	}
	if len(plan.Unmatched) != 0 || len(plan.Absent) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

// Le cas rapporté : GitHub Classroom a doublé le tiret — « …-KickMyB--alice » —,
// et deux dépôts sur trente-cinq ne donnaient accès à personne. Le travail se
// dédoublait alors : « TP3-H23-4204N6-KickMyB- » pour ceux dont le compte était
// connu, « tp3-h23-4204n6-kickmyb » pour les autres. Un seul et même travail,
// que l'écran demandait de départager.
func TestImportNeDedoublePasUnTravailAuTiretDouble(t *testing.T) {
	inventaire := depots(
		"TP3-H23-4204N6-KickMyB--laubelachance",
		"TP3-H23-4204N6-KickMyB--felixb",
		"TP3-H23-4204N6-KickMyB--loiselle",
	)

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		// Le préfixe deviné arrive en minuscules, comme l'écran le propose.
		Prefix: "tp3-h23-4204n6-kickmyb", Entries: inscrits(), Guess: true,
		// Deux dépôts livrent leur compte, le troisième non.
		Owners: map[string]string{
			"TP3-H23-4204N6-KickMyB--laubelachance": "laubelachance",
			"TP3-H23-4204N6-KickMyB--felixb":        "felixb",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if plan.Divided() {
		t.Fatalf("un seul travail attendu, %d proposés : %+v", len(plan.Splits), plan.Splits)
	}
	if len(plan.Moves) != 3 {
		t.Fatalf("les trois dépôts doivent être repris : %+v", plan.Moves)
	}
	// Le compte du dépôt sans accès est lu dans son nom, sans le tiret qui le
	// précédait : « -loiselle » n'est pas un compte GitHub.
	for _, trouve := range plan.Pairings {
		if strings.HasPrefix(trouve.Login, "-") {
			t.Fatalf("compte mal découpé : %+v", trouve)
		}
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["TP3-H23-4204N6-KickMyB--loiselle"] !=
		"a26.5n6.1030.tp3-h23-4204n6-kickmyb.edouard-loiselle" {
		t.Fatalf("cibles = %v", cibles)
	}
}

// Le cas rapporté : quatre étudiants associés par le registre étaient déclarés
// absents du même souffle. Le registre garde le nom du profil GitHub, la liste
// d'Omnivox le nom officiel — la même personne, comptée deux fois.
func TestImportNeDitPasAbsentQuiVientDetreAssocie(t *testing.T) {
	inventaire := depots("tp3-anwarlamari", "tp3-ggosselinbrault", "tp3-felixb")
	liste := []roster.Entry{
		{FullName: "Anwar Wasim Lamari", StudentID: "111"},
		{FullName: "Guillermo Gosselin-Brault", StudentID: "222"},
		{FullName: "Félix Boisvert", StudentID: "333"},
		{FullName: "Sophie Tremblay", StudentID: "444"}, // celle-là n'a pas de dépôt
	}

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp3", Entries: liste, Guess: true,
		Known: map[string]string{
			"anwarlamari":     "Anwar Lamari",
			"ggosselinbrault": "Guillermo Brault",
			"felixb":          "Félix Boisvert",
		},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	// Seule celle qui n'a vraiment aucun dépôt est absente.
	if len(plan.Absent) != 1 || plan.Absent[0] != "Sophie Tremblay" {
		t.Fatalf("absents = %v", plan.Absent)
	}
	// Et le nom retenu est celui de la liste, avec son numéro d'étudiant.
	for _, trouve := range plan.Pairings {
		if trouve.Login == "anwarlamari" {
			if trouve.Entry.FullName != "Anwar Wasim Lamari" || trouve.Entry.StudentID != "111" {
				t.Fatalf("entrée retenue = %+v", trouve.Entry)
			}
		}
	}
	cibles := map[string]string{}
	for _, ligne := range plan.Moves {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["tp3-anwarlamari"] != "a26.5n6.1030.tp3.anwar-wasim-lamari" {
		t.Fatalf("cibles = %v", cibles)
	}
}

// Une accentuation ou une casse ne fait pas une personne de plus.
func TestImportNeCompteQuUneFoisUnNomEcritAutrement(t *testing.T) {
	inventaire := depots("tp1-fboisvert")
	liste := []roster.Entry{{FullName: "FÉLIX BOISVERT", StudentID: "333"}}

	plan, err := classroom.PlanImport(arrivee(), classroom.ImportRequest{
		Prefix: "tp1", Entries: liste, Guess: true,
		Known: map[string]string{"fboisvert": "Felix Boisvert"},
	}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(plan.Absent) != 0 {
		t.Fatalf("absents = %v", plan.Absent)
	}
}
