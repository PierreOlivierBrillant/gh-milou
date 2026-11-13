package roster_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// cohorte reprend la forme des noms qu'une liste de cégep porte vraiment :
// traits d'union, accents, particules, noms composés en deux mots.
func cohorte() []roster.Entry {
	return []roster.Entry{
		{FullName: "Lorenzo Aube-Lachance", StudentID: "1600105", Permanent: "AUBL10109901"},
		{FullName: "Félix Boisvert", StudentID: "2100106", Permanent: "BOIF10109902"},
		{FullName: "Édouard Loiselle", StudentID: "1900107", Permanent: "LOIE10109903"},
		{FullName: "Jacob Sicard-Legris", StudentID: "1900108", Permanent: "SICJ10109904"},
		{FullName: "Idris El Hamdani", StudentID: "2100103", Permanent: "ELHI10109905"},
		{FullName: "Dante Gaspar Rivas", StudentID: "1900102", Permanent: "GASD10109906"},
	}
}

// trouve retrouve un rapprochement par son compte.
func trouve(t *testing.T, rapprochements []roster.Pairing, login string) roster.Pairing {
	t.Helper()
	for _, r := range rapprochements {
		if r.Login == login {
			return r
		}
	}
	t.Fatalf("aucun rapprochement pour « %s »", login)
	return roster.Pairing{}
}

// Les pseudonymes qu'on rencontre vraiment, du plus explicite au plus avare.
func TestRapprochementDesFormesCourantes(t *testing.T) {
	cas := []struct {
		login   string
		nom     string
		minimum int
	}{
		{"lorenzo-aube-lachance", "Lorenzo Aube-Lachance", roster.Certain},
		{"felixboisvert", "Félix Boisvert", roster.Probable},
		{"laubelachance", "Lorenzo Aube-Lachance", roster.Probable},
		{"loiselle", "Édouard Loiselle", roster.Probable},
		{"jsicardlegris", "Jacob Sicard-Legris", roster.Probable},
		// La particule « El » ne compte pas : c'est « hamdani » qui désigne.
		{"ihamdani", "Idris El Hamdani", roster.Probable},
		// Un nom en deux mots : le plus long tranche.
		{"drivas", "Dante Gaspar Rivas", roster.Probable},
	}
	for _, essai := range cas {
		t.Run(essai.login, func(t *testing.T) {
			// Un compte à la fois : c'est le score qui est éprouvé ici, non
			// la répartition entre plusieurs comptes d'une même personne.
			trouvee := trouve(t, roster.Match(cohorte(), []string{essai.login}, nil), essai.login)
			if trouvee.Entry.FullName != essai.nom {
				t.Fatalf("« %s » rapproché de %q (%d, %s), attendu %q",
					essai.login, trouvee.Entry.FullName, trouvee.Score, trouvee.Reason, essai.nom)
			}
			if trouvee.Score < essai.minimum {
				t.Errorf("score = %d (%s), attendu au moins %d",
					trouvee.Score, trouvee.Reason, essai.minimum)
			}
		})
	}
}

// Le numéro d'étudiant dans un pseudonyme n'est pas une coïncidence : c'est
// l'indice le plus sûr, et il l'emporte même quand le nom ne dit rien.
func TestLeNumeroDEtudiantTrancheSeul(t *testing.T) {
	rapprochements := roster.Match(cohorte(), []string{"xyz1600105"}, nil)
	trouvee := trouve(t, rapprochements, "xyz1600105")
	if trouvee.Entry.FullName != "Lorenzo Aube-Lachance" || trouvee.Score != 100 {
		t.Fatalf("rapprochement = %+v", trouvee)
	}
	if !strings.Contains(trouvee.Reason, "numéro") {
		t.Errorf("raison = %q", trouvee.Reason)
	}
}

// Le nom du profil GitHub, quand on le connaît, vaut mieux que le pseudonyme.
func TestLeProfilGitHubTrancheQuandLePseudoNeDitRien(t *testing.T) {
	profils := map[string]string{"xkcd42": "Édouard Loiselle"}
	rapprochements := roster.Match(cohorte(), []string{"xkcd42"}, profils)
	trouvee := trouve(t, rapprochements, "xkcd42")
	if trouvee.Entry.FullName != "Édouard Loiselle" || trouvee.Score < roster.Certain {
		t.Fatalf("rapprochement = %+v", trouvee)
	}
}

// Un pseudonyme que rien ne relie à personne reste sans réponse : inventer un
// rapprochement serait pire que de n'en proposer aucun.
func TestUnPseudoOpaqueNeSeRapprochePas(t *testing.T) {
	rapprochements := roster.Match(cohorte(), []string{"dark-wizard-99"}, nil)
	trouvee := trouve(t, rapprochements, "dark-wizard-99")
	if trouvee.Found() {
		t.Fatalf("rapprochement inventé : %+v", trouvee)
	}
}

// Deux personnes qui se valent ne se départagent pas : les nommer vaut mieux
// que d'en retenir une au hasard.
func TestDeuxPersonnesQuiSeValentSontSignalees(t *testing.T) {
	jumeaux := []roster.Entry{
		{FullName: "Marc Tremblay", StudentID: "1111111"},
		{FullName: "Marie Tremblay", StudentID: "2222222"},
	}
	rapprochements := roster.Match(jumeaux, []string{"tremblay"}, nil)
	trouvee := trouve(t, rapprochements, "tremblay")
	if !trouvee.Ambiguous || trouvee.Found() {
		t.Fatalf("rapprochement = %+v", trouvee)
	}
	if len(trouvee.Rivals) != 2 {
		t.Fatalf("rivales = %v", trouvee.Rivals)
	}
}

// Une personne n'est retenue qu'une fois, et c'est l'indice le plus sûr qui la
// prend : un pseudonyme vague ne vole pas la place d'un pseudonyme explicite.
func TestUnePersonneNEstRetenueQuUneFois(t *testing.T) {
	gens := []roster.Entry{
		{FullName: "Félix Boisvert", StudentID: "2100106"},
		{FullName: "Olivier Boisclair", StudentID: "2100109"},
	}
	// « boisvert » ressemble aux deux ; « felixboisvert2100106 » ne ressemble qu'à un.
	rapprochements := roster.Match(gens, []string{"boisvert", "felixboisvert2100106"}, nil)
	sur := trouve(t, rapprochements, "felixboisvert2100106")
	if sur.Entry.FullName != "Félix Boisvert" || sur.Score != 100 {
		t.Fatalf("le compte le plus explicite = %+v", sur)
	}
	vague := trouve(t, rapprochements, "boisvert")
	if vague.Entry.FullName == "Félix Boisvert" {
		t.Fatalf("la même personne a été retenue deux fois : %+v", vague)
	}
}

// Deux exécutions disent la même chose : l'ordre ne dépend pas du hasard des
// tables de hachage.
func TestLeRapprochementEstStable(t *testing.T) {
	logins := []string{"laubelachance", "loiselle", "drivas", "inconnu"}
	premier := roster.Match(cohorte(), logins, nil)
	for essai := 0; essai < 5; essai++ {
		suivant := roster.Match(cohorte(), logins, nil)
		for index := range premier {
			if premier[index].Login != suivant[index].Login ||
				premier[index].Entry.FullName != suivant[index].Entry.FullName {
				t.Fatalf("rapprochement instable : %+v puis %+v", premier, suivant)
			}
		}
	}
	// L'ordre rendu est celui des comptes fournis.
	if premier[0].Login != "laubelachance" || premier[3].Login != "inconnu" {
		t.Fatalf("ordre = %+v", premier)
	}
}

func TestFindNameReconnaitDeuxEcrituresDunMemeNom(t *testing.T) {
	liste := []roster.Entry{
		{FullName: "Anwar Wasim Lamari", StudentID: "111"},
		{FullName: "Guillermo Gosselin-Brault", StudentID: "222"},
		{FullName: "Félix Boisvert", StudentID: "333"},
	}
	cas := []struct {
		cherche, attendu string
	}{
		// Le registre garde le nom du profil GitHub, plus court que l'officiel.
		{"Anwar Lamari", "Anwar Wasim Lamari"},
		{"Guillermo Brault", "Guillermo Gosselin-Brault"},
		// L'écriture exacte, à l'accent et à la casse près.
		{"FELIX BOISVERT", "Félix Boisvert"},
		{"Felix Boisvert", "Félix Boisvert"},
	}
	for _, essai := range cas {
		trouve, dans := roster.FindName(liste, essai.cherche)
		if !dans || trouve.FullName != essai.attendu {
			t.Errorf("FindName(%q) = %q, %v ; attendu %q",
				essai.cherche, trouve.FullName, dans, essai.attendu)
		}
	}
}

func TestFindNameNeTranchePasDansLeDoute(t *testing.T) {
	liste := []roster.Entry{
		{FullName: "Jean Tremblay"},
		{FullName: "Jean Marc Tremblay"},
	}
	// Deux personnes s'y prêtent : mieux vaut n'en nommer aucune.
	if trouve, dans := roster.FindName(liste, "Jean Tremblay"); !dans ||
		trouve.FullName != "Jean Tremblay" {
		t.Fatalf("l'écriture exacte doit primer : %q, %v", trouve.FullName, dans)
	}
	if trouve, dans := roster.FindName(liste, "Tremblay Jean Marc Junior"); dans {
		t.Fatalf("FindName = %q : deux personnes s'y prêtaient", trouve.FullName)
	}
	// Un prénom seul ne désigne personne.
	if trouve, dans := roster.FindName([]roster.Entry{{FullName: "Anwar Wasim Lamari"}},
		"Anwar"); dans {
		t.Fatalf("FindName = %q : un mot seul ne suffit pas", trouve.FullName)
	}
	if _, dans := roster.FindName(liste, ""); dans {
		t.Fatal("un nom vide ne désigne personne")
	}
}
