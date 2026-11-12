package groups_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
)

func TestDetectPrefixesLesPlusGeneraux(t *testing.T) {
	noms := []string{
		"tp1-emilie-cote", "tp1-jlpicard", "tp1-aminata-d",
		"projet-final-emilie-cote", "projet-final-jlpicard",
		"notes-personnelles",
	}
	detectes := groups.Detect(noms, 2)
	attendu := []groups.Detected{
		{Prefix: "tp1", Count: 3},
		{Prefix: "projet-final", Count: 2},
	}
	if !reflect.DeepEqual(detectes, attendu) {
		t.Fatalf("Detect = %+v, attendu %+v", detectes, attendu)
	}
}

func TestDetectIgnoreLesGroupesTropPetits(t *testing.T) {
	if detectes := groups.Detect([]string{"tp1-seul", "autre-chose"}, 2); len(detectes) != 0 {
		t.Fatalf("Detect = %+v, aucun groupe attendu", detectes)
	}
}

func TestDetectEtendAuPlusLongPrefixeCommun(t *testing.T) {
	noms := []string{"projet-final-a", "projet-final-b", "projet-final-c"}
	detectes := groups.Detect(noms, 2)
	if len(detectes) != 1 || detectes[0].Prefix != "projet-final" || detectes[0].Count != 3 {
		t.Fatalf("Detect = %+v", detectes)
	}
}

func TestDetectDeuxTravauxDuMemePrefixe(t *testing.T) {
	noms := []string{"tp1-a", "tp1-b", "tp2-a", "tp2-b"}
	detectes := groups.Detect(noms, 2)
	if len(detectes) != 2 {
		t.Fatalf("Detect = %+v", detectes)
	}
	vus := map[string]int{}
	for _, item := range detectes {
		vus[item.Prefix] = item.Count
	}
	if vus["tp1"] != 2 || vus["tp2"] != 2 {
		t.Errorf("groupes = %+v", vus)
	}
}

// parPrefixe indexe les groupes détectés par préfixe, pour des attentes lisibles.
func parPrefixe(detectes []groups.Detected) map[string]int {
	tailles := map[string]int{}
	for _, item := range detectes {
		tailles[item.Prefix] = item.Count
	}
	return tailles
}

func TestDetectSepareDeuxTravauxSousUnPrefixeCommun(t *testing.T) {
	noms := []string{
		"a26-5n6-travailsession-emilie-cote", "a26-5n6-travailsession-jlpicard",
		"a26-4w6-tp1-aminata-d", "a26-4w6-tp1-jlpicard",
	}
	tailles := parPrefixe(groups.Detect(noms, 2))
	attendu := map[string]int{"a26-5n6-travailsession": 2, "a26-4w6-tp1": 2}
	if !reflect.DeepEqual(tailles, attendu) {
		t.Fatalf("groupes = %+v, attendu %+v", tailles, attendu)
	}
}

func TestDetectSepareMalgreUnDepotIsole(t *testing.T) {
	// Un dépôt qui s'arrête au préfixe commun — des notes du cours, par exemple —
	// ne doit pas refondre les deux travaux dans « a26 ».
	noms := []string{
		"a26-5n6-travailsession-emilie-cote", "a26-5n6-travailsession-jlpicard",
		"a26-4w6-tp1-aminata-d", "a26-4w6-tp1-jlpicard",
		"a26-notes",
	}
	tailles := parPrefixe(groups.Detect(noms, 2))
	attendu := map[string]int{"a26-5n6-travailsession": 2, "a26-4w6-tp1": 2}
	if !reflect.DeepEqual(tailles, attendu) {
		t.Fatalf("groupes = %+v, attendu %+v", tailles, attendu)
	}
}

func TestDetectNeSubdivisePasUnGroupeDeComptes(t *testing.T) {
	// Des comptes GitHub qui partagent un premier segment ne font pas des
	// sous-groupes : « tp1 » reste entier.
	noms := []string{
		"tp1-emilie-cote", "tp1-emilie-roy", "tp1-marie-curie", "tp1-marie-eve",
		"tp1-jlpicard", "tp1-adiallo",
	}
	tailles := parPrefixe(groups.Detect(noms, 2))
	if !reflect.DeepEqual(tailles, map[string]int{"tp1": 6}) {
		t.Fatalf("groupes = %+v, attendu tp1 avec 6 dépôts", tailles)
	}
}

func TestBuildGroupe(t *testing.T) {
	repos := []groups.RepoInfo{
		{Name: "tp1-jlpicard", Private: true, HTMLURL: "https://github.com/acme/tp1-jlpicard", PushedAt: "2026-08-19T10:11:12Z"},
		{Name: "tp1-emilie-cote", Private: false, HTMLURL: "https://github.com/acme/tp1-emilie-cote"},
		{Name: "autre-depot", Private: true},
		{Name: "tp1", Private: true},
	}
	groupe := groups.Build("TP1", repos)
	if groupe.Prefix != "tp1" || groupe.Len() != 2 {
		t.Fatalf("groupe = %+v", groupe)
	}
	if groupe.Repos[0].Name != "tp1-emilie-cote" {
		t.Errorf("les dépôts doivent être triés : %+v", groupe.Repos)
	}
	if groupe.Repos[0].Suffix != "emilie-cote" || groupe.Repos[0].Visibility() != "public" {
		t.Errorf("premier dépôt = %+v", groupe.Repos[0])
	}
	// Le dernier envoi porte l'heure, et à celle de la machine : GitHub le
	// date en UTC, et une remise du soir paraîtrait faite le lendemain.
	attendu := time.Date(2026, 8, 19, 10, 11, 12, 0, time.UTC).Local().Format("2006-01-02 15:04")
	if groupe.Repos[1].PushedAt != attendu {
		t.Errorf("date = %q, attendu %q", groupe.Repos[1].PushedAt, attendu)
	}
	if _, index, ok := groupe.Find("JLPICARD"); !ok || index != 1 {
		t.Errorf("Find par suffixe = %d, %v", index, ok)
	}
	if _, _, ok := groupe.Find("tp1-inconnu"); ok {
		t.Error("Find doit échouer sur un nom absent")
	}
}

// Un préfixe se saisit sans avoir à dire ce qui le termine : le tiret des noms
// qu'aucune convention n'organise, ou le point de la nomenclature à cinq
// niveaux. Sans cela, l'assistant du terminal ne pourrait ouvrir aucun travail
// nommé comme l'outil les nomme.
func TestBuildLitLesDeuxNomenclatures(t *testing.T) {
	repos := []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.emilie-cote"},
		{Name: "a26.5n6.01.tp1.jlpicard"},
		{Name: "a26.5n6.01.tp2.jlpicard"},
		{Name: "a26.5n6.02.tp1.jlpicard"},
	}
	travail := groups.Build("a26.5n6.01.tp1", repos)
	if travail.Len() != 2 || travail.Repos[0].Suffix != "emilie-cote" {
		t.Fatalf("travail = %+v", travail)
	}
	// Le préfixe d'un groupe retient tous ses travaux, et rien du groupe voisin.
	if groupe := groups.Build("a26.5n6.01", repos); groupe.Len() != 3 {
		t.Fatalf("groupe = %+v", groupe)
	}
	// Un préfixe qui s'arrête au milieu d'un niveau ne retient rien.
	if partiel := groups.Build("a26.5n6.01.tp", repos); partiel.Len() != 0 {
		t.Fatalf("préfixe partiel = %+v", partiel)
	}
}

func TestBuildPrefixeVide(t *testing.T) {
	if groupe := groups.Build("  ", []groups.RepoInfo{{Name: "tp1-a"}}); groupe.Len() != 0 {
		t.Errorf("un préfixe vide ne doit rien retenir : %+v", groupe)
	}
}

func TestParseSelection(t *testing.T) {
	noms := []string{"tp1-a", "tp1-b", "tp1-c", "tp1-d", "tp1-e"}
	resolve := func(token string) int {
		for index, nom := range noms {
			if nom == token {
				return index
			}
		}
		return -1
	}
	cas := map[string][]int{
		"":            {0, 1, 2, 3, 4},
		"tous":        {0, 1, 2, 3, 4},
		"ALL":         {0, 1, 2, 3, 4},
		"1,3":         {0, 2},
		"2-4":         {1, 2, 3},
		"4-2":         {1, 2, 3},
		"1, 3-4":      {0, 2, 3},
		"tp1-c":       {2},
		"1,3-4,tp1-e": {0, 2, 3, 4},
		"2 2 2":       {1},
	}
	for entree, attendu := range cas {
		obtenu, err := groups.ParseSelection(entree, len(noms), resolve)
		if err != nil {
			t.Errorf("ParseSelection(%q) : %v", entree, err)
			continue
		}
		if !reflect.DeepEqual(obtenu, attendu) {
			t.Errorf("ParseSelection(%q) = %v, attendu %v", entree, obtenu, attendu)
		}
	}
}

func TestParseSelectionInvalide(t *testing.T) {
	for _, entree := range []string{"0", "6", "1-9", "inconnu", "-3"} {
		if _, err := groups.ParseSelection(entree, 5, func(string) int { return -1 }); err == nil {
			t.Errorf("ParseSelection(%q) devrait échouer", entree)
		}
	}
}

func TestDetectListeVideOuUnique(t *testing.T) {
	if detectes := groups.Detect(nil, 2); len(detectes) != 0 {
		t.Errorf("Detect(nil) = %+v", detectes)
	}
	if detectes := groups.Detect([]string{"tp1-seul"}, 2); len(detectes) != 0 {
		t.Errorf("un seul dépôt ne fait pas un groupe : %+v", detectes)
	}
	if detectes := groups.Detect([]string{"sansseparateur", "autre"}, 2); len(detectes) != 0 {
		t.Errorf("des noms sans séparateur ne font pas un groupe : %+v", detectes)
	}
}

func TestParseSelectionListeVide(t *testing.T) {
	if indices, err := groups.ParseSelection("tous", 0, nil); err != nil || len(indices) != 0 {
		t.Errorf("ParseSelection = %v, %v", indices, err)
	}
}

// Les dépôts de service de l'organisation ne sont pas des dépôts d'étudiants :
// « .github » porte les gabarits communs, et l'outil y rangera le sien.
func TestDepotsDeServiceEcartes(t *testing.T) {
	inventaire := []groups.RepoInfo{
		{Name: ".github"},
		{Name: "a26.5n6.01.tp1.emilie-cote"},
		{Name: ".cohorte"},
		{Name: "tp1-jlpicard"},
	}
	restants := groups.Ordinary(inventaire)
	if len(restants) != 2 ||
		restants[0].Name != "a26.5n6.01.tp1.emilie-cote" || restants[1].Name != "tp1-jlpicard" {
		t.Fatalf("inventaire retenu = %+v", restants)
	}
	// Un point ailleurs qu'en tête sépare les niveaux : ce n'est pas un service.
	if groups.Service("a26.5n6.01.tp1.emilie-cote") {
		t.Error("un nom de la nomenclature n'est pas un dépôt de service")
	}
}

// Renommer un dépôt n'est pas y pousser : sa date de dernier envoi ne bouge
// pas. C'est ce qui permet de suivre l'inventaire au lieu de le relire.
func TestInventaireSuitUnRenommage(t *testing.T) {
	inventaire := []groups.RepoInfo{
		{Name: "a26.5n6.01.tp1.jlpicard", HTMLURL: "https://x/tp1", PushedAt: "2026-09-01"},
		{Name: "a26.5n6.01.tp2.jlpicard", HTMLURL: "https://x/tp2", PushedAt: "2026-09-02"},
	}
	suivi := groups.WithRenamed(inventaire, []groups.Renamed{{
		Before: "a26.5n6.01.tp1.jlpicard",
		After: groups.RepoInfo{
			Name: "a26.5n6.01.projet.jlpicard", HTMLURL: "https://x/projet", Private: true},
	}})
	if suivi[0].Name != "a26.5n6.01.projet.jlpicard" || suivi[0].HTMLURL != "https://x/projet" {
		t.Fatalf("dépôt renommé = %+v", suivi[0])
	}
	if suivi[0].PushedAt != "2026-09-01" {
		t.Errorf("dernier envoi = %q : renommer n'est pas pousser", suivi[0].PushedAt)
	}
	if suivi[1] != inventaire[1] {
		t.Errorf("un dépôt non renommé a bougé : %+v", suivi[1])
	}
	// L'inventaire de départ n'est pas touché : deux lecteurs peuvent le partager.
	if inventaire[0].Name != "a26.5n6.01.tp1.jlpicard" {
		t.Error("l'inventaire d'origine a été modifié en place")
	}
}

func TestInventaireSansUnDepotSupprime(t *testing.T) {
	inventaire := []groups.RepoInfo{{Name: "tp1-a"}, {Name: "TP1-B"}, {Name: "tp1-c"}}
	// La casse d'un nom de dépôt n'en fait pas un autre dépôt.
	restants := groups.WithoutRepo(inventaire, "tp1-b")
	if len(restants) != 2 || restants[0].Name != "tp1-a" || restants[1].Name != "tp1-c" {
		t.Fatalf("inventaire restant = %+v", restants)
	}
	if len(groups.WithoutRepo(inventaire, "absent")) != 3 {
		t.Error("retirer un dépôt absent ne doit rien changer")
	}
}

func TestSplitRetireLeCompteQuiTermineLeNom(t *testing.T) {
	cas := []struct {
		nom, compte, travail string
		coupe                bool
	}{
		// Le cas qui motive la fonction : deux tirets dans le travail, un dans
		// le nom du compte, et rien dans le nom seul ne dit où couper.
		{"kickmyb-firebase-Walid7Akk", "Walid7Akk", "kickmyb-firebase", true},
		{"tp1-jlpicard", "jlpicard", "tp1", true},
		{"tp1-JLPicard", "jlpicard", "tp1", true},
		{"a26.5n6.01.tp1.emilie-cote", "emilie-cote", "a26.5n6.01.tp1", true},
		// Le compte doit terminer le nom, et être détaché.
		{"tp1-jlpicard-bis", "jlpicard", "", false},
		{"tp1jlpicard", "jlpicard", "", false},
		{"jlpicard", "jlpicard", "", false},
		{"tp1-jlpicard", "", "", false},
	}
	for _, essai := range cas {
		travail, coupe := groups.Split(essai.nom, essai.compte)
		if coupe != essai.coupe || travail != essai.travail {
			t.Errorf("Split(%q, %q) = %q, %v ; attendu %q, %v",
				essai.nom, essai.compte, travail, coupe, essai.travail, essai.coupe)
		}
	}
}

func TestOwnerPrefereLeCompteQueLeNomPorte(t *testing.T) {
	// L'enseignant a aussi accès au dépôt : c'est le nom qui tranche.
	compte, sur := groups.Owner("kickmyb-firebase-Walid7Akk",
		[]string{"prof-cegep", "Walid7Akk"})
	if !sur || compte != "Walid7Akk" {
		t.Fatalf("Owner = %q, %v", compte, sur)
	}
}

func TestOwnerPrendLeCompteLePlusLong(t *testing.T) {
	compte, sur := groups.Owner("tp1-walid7akk", []string{"akk", "walid7akk"})
	if !sur || compte != "walid7akk" {
		t.Fatalf("Owner = %q, %v", compte, sur)
	}
}

func TestOwnerSeContenteDunAccesUnique(t *testing.T) {
	// Rien dans « projet-equipe-3 » ne nomme personne : l'accès seul suffit.
	compte, sur := groups.Owner("projet-equipe-3", []string{"emilie-cote"})
	if !sur || compte != "emilie-cote" {
		t.Fatalf("Owner = %q, %v", compte, sur)
	}
}

func TestOwnerNeTranchePasEntreDeuxInconnus(t *testing.T) {
	if compte, sur := groups.Owner("projet-equipe-3",
		[]string{"emilie-cote", "jlpicard"}); sur {
		t.Fatalf("Owner = %q, %v : rien ne désigne l'un plutôt que l'autre", compte, sur)
	}
	if compte, sur := groups.Owner("tp1-personne", nil); sur {
		t.Fatalf("Owner = %q, %v : aucun accès ne mène à personne", compte, sur)
	}
}

func TestSplitNeGardePasLeSeparateurEnTrop(t *testing.T) {
	// GitHub Classroom double parfois le tiret : un seul sépare.
	travail, coupe := groups.Split("TP3-H23-4204N6-KickMyB--alice", "alice")
	if !coupe || travail != "TP3-H23-4204N6-KickMyB" {
		t.Fatalf("Split = %q, %v", travail, coupe)
	}
	// Un nom qui ne serait que des séparateurs et un compte ne dit aucun travail.
	if travail, coupe := groups.Split("--alice", "alice"); coupe {
		t.Fatalf("Split = %q, %v", travail, coupe)
	}
}

func TestBuildNeGardePasLeSeparateurEnTeteDuSuffixe(t *testing.T) {
	// GitHub Classroom double parfois le tiret : le second n'est à personne.
	depots := []groups.RepoInfo{
		{Name: "TP3-H23-4204N6-KickMyB--ahmadloudin"},
		{Name: "TP3-H23-4204N6-KickMyB--felixb"},
	}
	// Le préfixe arrive tel que la détection le propose, tiret final compris.
	groupe := groups.Build("tp3-h23-4204n6-kickmyb-", depots)
	if groupe.Len() != 2 {
		t.Fatalf("dépôts = %+v", groupe.Repos)
	}
	for _, depot := range groupe.Repos {
		if strings.HasPrefix(depot.Suffix, "-") {
			t.Fatalf("suffixe = %q : un compte GitHub ne commence pas par un tiret",
				depot.Suffix)
		}
	}
	if _, _, trouve := groupe.Find("ahmadloudin"); !trouve {
		t.Fatalf("le dépôt ne se retrouve pas par son compte : %+v", groupe.Repos)
	}
}
