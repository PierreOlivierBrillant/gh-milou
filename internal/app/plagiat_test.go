package app_test

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/anonymize"
	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
)

const gabaritTP = `
package tp1;

import java.util.ArrayList;
import java.util.List;

public class Inventaire {
    private final List<String> articles = new ArrayList<>();

    public void ajouter(String article) {
        if (article == null || article.isEmpty()) {
            throw new IllegalArgumentException("article vide");
        }
        articles.add(article);
    }

    public boolean contient(String article) {
        return articles.contains(article);
    }

    public int taille() {
        return articles.size();
    }
}
`

const solutionTP = `
public class Solution {
    public int somme(int[] valeurs) {
        int total = 0;
        for (int index = 0; index < valeurs.length; index++) {
            total = total + valeurs[index];
        }
        return total;
    }

    public int maximum(int[] valeurs) {
        int plusGrand = valeurs[0];
        for (int index = 1; index < valeurs.length; index++) {
            if (valeurs[index] > plusGrand) {
                plusGrand = valeurs[index];
            }
        }
        return plusGrand;
    }
}
`

const autreTP = `
import java.util.Arrays;

public class Travail {
    public int somme(int[] entrees) {
        return Arrays.stream(entrees).sum();
    }

    public int maximum(int[] entrees) {
        return Arrays.stream(entrees).max().orElseThrow();
    }
}
`

// groupeRemis monte un travail remis par trois personnes : deux copies
// identiques, une différente, toutes parties du même gabarit.
func groupeRemis(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	state.AddRepo("acme", "modele-tp1", true)
	state.SeedCommit("acme/modele-tp1",
		map[string]string{"src/Inventaire.java": gabaritTP}, "main")

	copies := map[string]string{
		"a26.5n6.01.tp1.emilie-cote":     solutionTP,
		"a26.5n6.01.tp1.jean-luc-picard": solutionTP,
		"a26.5n6.01.tp1.aminata-diallo":  autreTP,
	}
	// Des remises étalées : c'est ce qui permet de dire, sous le tableau des
	// paires, qui a remis en premier.
	remises := map[string]fakegh.HistoryEntry{
		"a26.5n6.01.tp1.emilie-cote":     {At: "2026-09-20T10:00:00Z", Login: "emilie-cote"},
		"a26.5n6.01.tp1.jean-luc-picard": {At: "2026-09-01T10:00:00Z", Login: "jlpicard"},
		"a26.5n6.01.tp1.aminata-diallo":  {At: "2026-09-12T08:00:00Z", Login: "aminata-d"},
	}
	for nom, source := range copies {
		depot := state.AddRepo("acme", nom, true)
		depot.History = []fakegh.HistoryEntry{remises[nom]}
		state.SeedCommit("acme/"+nom, map[string]string{
			"src/Inventaire.java": gabaritTP,
			"src/Solution.java":   source,
			"assets/logo.ico":     "\x00\x00 image",
		}, "main")
	}

	h := nouveau(t, state)
	defauts := classroom.DefaultsFrom(config.Default())
	defauts.Template = "acme/modele-tp1"
	store := classroom.Open(classroom.PathNextTo(h.Reglages))
	cours := classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Defaults: defauts,
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Aminata Diallo", Username: "aminata-d"},
		},
	}
	if _, err := store.Save(cours); err != nil {
		t.Fatalf("déclaration : %v", err)
	}
	return h
}

func TestDrapeauPlagiarismCompareLesCopiesEtEcritSonRapport(t *testing.T) {
	h := groupeRemis(t)
	h.Options.Plagiarism = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	// L'avertissement est dit, et il n'est pas raccourci : c'est le même texte
	// que le navigateur affiche, décidé dans le domaine.
	h.contient("n'est pas un détecteur de plagiat")
	// Les deux copies identiques sont appariées, et nommées par leur nom
	// complet plutôt que par le slug du dépôt.
	h.contient("Émilie Côté", "Jean-Luc Picard", "Paires, du plus suspect")
	// Le gabarit distribué est retiré de lui-même : l'outil sait lequel il a
	// déposé.
	h.contient("venant du gabarit distribué")
	// La vue de comparaison n'existe qu'au navigateur : le trou est dit plutôt
	// que laissé en silence.
	h.contient("n'a pas d'équivalent au terminal")

	// Le rapport est un fichier, et l'assistant dit où il est.
	rapports := plagiarism.List(h.Rapports)
	if len(rapports) != 1 {
		t.Fatalf("rapports écrits : %v", rapports)
	}
	rapport, err := plagiarism.Load(rapports[0])
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if rapport.Analyzed() != 3 {
		t.Fatalf("copies analysées : %d", rapport.Analyzed())
	}
	match, trouve := rapport.Match(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard")
	if !trouve || match.Similarity < 0.99 {
		t.Fatalf("deux copies identiques : %.2f (trouvée : %v)", match.Similarity, trouve)
	}
	// Aminata a résolu l'exercice autrement : elle ne doit pas remonter.
	if match, trouve := rapport.Match(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.aminata-diallo"); trouve &&
		match.Similarity > 0.3 {
		t.Fatalf("une solution différente remonte à %.2f", match.Similarity)
	}

	// Le CSV est écrit à côté, pour un tableur ou un dossier.
	csv := strings.TrimSuffix(rapports[0], ".json") + ".csv"
	if _, err := os.Stat(csv); err != nil {
		t.Fatalf("CSV des paires : %v", err)
	}
	if !strings.Contains(rapports[0], filepath.Join(h.Rapports, plagiarism.Dir)) {
		t.Fatalf("emplacement du rapport : %q", rapports[0])
	}
}

func TestDrapeauPlagiarismRefuseUnProfilInconnuSansRienTelecharger(t *testing.T) {
	h := groupeRemis(t)
	h.Options.Plagiarism = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Profile = "nextjs"
	h.Options.Yes = true

	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("nextjs")
	if len(plagiarism.List(h.Rapports)) != 0 {
		t.Fatal("un réglage refusé ne doit rien écrire")
	}
}

// Un langage retiré doit vraiment l'être : c'est le remède que l'estimation
// propose quand l'analyse est trop lourde, et il ne servirait à rien s'il ne
// changeait pas le corpus.
func TestDrapeauLanguagesRestreintCeQuiEstCompare(t *testing.T) {
	h := groupeRemis(t)
	h.Options.Plagiarism = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Languages = "sql"
	h.Options.Yes = true

	// Aucun fichier SQL dans ces dépôts : toutes les copies sont écartées, et
	// l'analyse doit le dire plutôt que de rendre un rapport vide sans motif.
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("aucun fichier retenu")
}

// Le gabarit de passe automatisée se dépose sans jeton ni réseau : c'est un
// fichier à écrire, et rien d'autre.
func TestLeGabaritDePasseSeDeposeEtSExplique(t *testing.T) {
	h := nouveau(t, nil)
	chemin := filepath.Join(t.TempDir(), "plagiat.yml")
	h.Options.EmitWorkflowSet = true
	h.Options.EmitWorkflow = chemin

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("gabarit : %v", err)
	}
	// Ce qu'il doit dire avant d'être utilisé : le jeton qu'il demande, et la
	// protection sans laquelle il annule le cloisonnement.
	for _, attendu := range []string{
		"MILOU_TOKEN", "protégé en écriture", "--publish-index",
		"ni code ni nom",
		// Elle tourne toute seule, et il y a une chose qu'elle ne fera
		// jamais : trancher une demande à la place du propriétaire.
		"schedule:", "ne se tranche jamais ici",
	} {
		if !strings.Contains(string(contenu), attendu) {
			t.Fatalf("le gabarit doit dire « %s » :\n%s", attendu, contenu)
		}
	}
	// Un rapport porte des noms : la variante qui le rend est commentée, et
	// prévient.
	if !strings.Contains(string(contenu), "# - name: Comparer") {
		t.Fatalf("la variante doit rester commentée :\n%s", contenu)
	}

	// Un fichier déjà présent n'est pas écrasé : il a peut-être été retouché.
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("un gabarit existant doit être refusé (code %d)", code)
	}
	h.contient("existe déjà")
}

// Le chemin complet d'une levée du voile, au terminal : un index publié, un
// collègue qui mesure sans pouvoir lire, une demande, une décision, et l'archive
// d'une seule copie que le propriétaire enverra lui-même.
func TestUneDemandeSeDeposeSeTrancheEtProduitUneArchive(t *testing.T) {
	h := groupeRemis(t)
	h.Options.PublishIndex = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("publication : code = %d\n%s", code, h.texte())
	}

	// Le jeton que le collègue a mesuré : c'est la table écrite à la
	// publication qui le donne, et lui seul voyagera.
	jeton := premierJetonPublie(t, h.Rapports)

	// Le collègue dépose sa demande. Le catalogue dit à qui elle s'adresse.
	collegue := nouveauDansLeMemeDossier(t, h)
	collegue.State.Viewer = "collegue"
	collegue.Options.Ask = "a26.5n6.01.tp1:" + jeton
	collegue.Options.Reason = "deux copies quasi identiques"
	collegue.Options.ManageRequested = false
	if code := collegue.muet(); code != app.ExitOK {
		t.Fatalf("dépôt : code = %d\n%s", code, collegue.texte())
	}
	collegue.contient("déposée auprès de @prof")
	demande := identifiantDeDemande(t, collegue.texte())

	// Elle ne s'adresse pas au collègue : il ne peut pas la trancher lui-même.
	sien := nouveauDansLeMemeDossier(t, h)
	sien.State.Viewer = "collegue"
	sien.Options.Deny = demande
	sien.Options.ManageRequested = false
	if code := sien.muet(); code != app.ExitValidation {
		t.Fatalf("un refus par le demandeur doit être écarté (code %d)\n%s",
			code, sien.texte())
	}
	sien.contient("s'adresse à @prof")

	// Le propriétaire accorde : une seule copie part, sous le jeton mesuré.
	archive := filepath.Join(t.TempDir(), "envoi.zip")
	prof := nouveauDansLeMemeDossier(t, h)
	prof.State.Viewer = "prof"
	prof.Options.Grant = demande
	prof.Options.ExportZip = archive
	prof.Options.ManageRequested = false
	prof.Options.Yes = true
	if code := prof.muet(); code != app.ExitOK {
		t.Fatalf("accord : code = %d\n%s", code, prof.texte())
	}
	prof.contient("accordée", "Envoyez")

	lecture, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatalf("archive : %v", err)
	}
	defer lecture.Close()
	copies := map[string]bool{}
	for _, fichier := range lecture.File {
		if racine, _, coupe := strings.Cut(fichier.Name, "/"); coupe {
			copies[racine] = true
		}
		// Ni nom ni compte : c'est tout l'objet de l'anonymisation.
		contenu := contenuDeZip(t, fichier)
		for _, interdit := range []string{"emilie-cote", "Émilie", "Picard"} {
			if strings.Contains(contenu, interdit) ||
				strings.Contains(fichier.Name, interdit) {
				t.Fatalf("« %s » reste dans %s", interdit, fichier.Name)
			}
		}
	}
	delete(copies, "")
	if len(copies) != 1 || !copies[jeton] {
		t.Fatalf("l'archive doit porter la seule copie « %s » : %v", jeton, copies)
	}
	// La table de correspondance ne voyage jamais : elle seule dit qui se cache
	// derrière le jeton.
	for _, fichier := range lecture.File {
		if strings.Contains(fichier.Name, "correspondance") {
			t.Fatalf("la table est partie dans l'archive : %s", fichier.Name)
		}
	}

	// La décision est écrite, et une demande tranchée ne se retranche pas.
	encore := nouveauDansLeMemeDossier(t, h)
	encore.State.Viewer = "prof"
	encore.Options.Deny = demande
	encore.Options.ManageRequested = false
	if code := encore.muet(); code != app.ExitValidation {
		t.Fatalf("une demande tranchée doit être écartée (code %d)\n%s",
			code, encore.texte())
	}
	encore.contient("déjà accordée")

	// La liste dit les deux côtés : ce qu'on nous demande, et ce qu'on demande.
	liste := nouveauDansLeMemeDossier(t, h)
	liste.State.Viewer = "prof"
	liste.Options.Requests = true
	liste.Options.ManageRequested = false
	if code := liste.muet(); code != app.ExitOK {
		t.Fatalf("liste : code = %d\n%s", code, liste.texte())
	}
	liste.contient("Demandes reçues", demande, "accordée")
}

// premierJetonPublie lit, dans la table écrite à la publication, le jeton d'une
// copie.
func premierJetonPublie(t *testing.T, rapports string) string {
	t.Helper()
	motif := filepath.Join(rapports, plagiarism.Dir,
		"*"+plagiarism.IndexTableSuffix)
	chemins, err := filepath.Glob(motif)
	if err != nil || len(chemins) == 0 {
		t.Fatalf("table d'index : %v (%v)", chemins, err)
	}
	contenu, err := os.ReadFile(chemins[0])
	if err != nil {
		t.Fatalf("table d'index : %v", err)
	}
	var table anonymize.Table
	if err := json.Unmarshal(contenu, &table); err != nil {
		t.Fatalf("table d'index : %v", err)
	}
	if len(table.Tokens) == 0 {
		t.Fatal("la table ne porte aucun jeton")
	}
	return table.Tokens[0].Base
}

// identifiantDeDemande retrouve, dans ce qui a été dit, l'identifiant déposé.
func identifiantDeDemande(t *testing.T, sortie string) string {
	t.Helper()
	trouve := regexp.MustCompile(`Demande ([A-Z0-9]{6}) déposée`).
		FindStringSubmatch(sortie)
	if trouve == nil {
		t.Fatalf("aucun identifiant de demande dans :\n%s", sortie)
	}
	return trouve[1]
}

// contenuDeZip lit un fichier de l'archive.
func contenuDeZip(t *testing.T, fichier *zip.File) string {
	t.Helper()
	lecture, err := fichier.Open()
	if err != nil {
		t.Fatalf("%s : %v", fichier.Name, err)
	}
	defer lecture.Close()
	contenu, err := io.ReadAll(lecture)
	if err != nil {
		t.Fatalf("%s : %v", fichier.Name, err)
	}
	return string(contenu)
}

// L'assistant ouvre le même écran, et le refus s'y prend en trois touches. Ce
// qui est vérifié ici, c'est le chemin : le menu du travail y mène, et la
// décision se retrouve au registre.
func TestLAssistantRefuseUneDemandeRecue(t *testing.T) {
	h := groupeRemis(t)
	h.Options.PublishIndex = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("publication : code = %d\n%s", code, h.texte())
	}
	jeton := premierJetonPublie(t, h.Rapports)

	collegue := nouveauDansLeMemeDossier(t, h)
	collegue.State.Viewer = "collegue"
	collegue.Options.Ask = "a26.5n6.01.tp1:" + jeton
	collegue.Options.ManageRequested = false
	if code := collegue.muet(); code != app.ExitOK {
		t.Fatalf("dépôt : code = %d\n%s", code, collegue.texte())
	}
	demande := identifiantDeDemande(t, collegue.texte())

	prof := nouveauDansLeMemeDossier(t, h)
	prof.State.Viewer = "prof"
	prof.Options.PublishIndex = false
	prof.Options.Manage = "a26.5n6.01.tp1"
	prof.Options.ManageRequested = true
	code, _ := prof.script("comparaisons", "demandes", "refuser", demande,
		"le dossier est déjà entre les mains de la direction", "retour", "quitter")
	if code != app.ExitOK {
		t.Fatalf("assistant : code = %d\n%s", code, prof.texte())
	}
	prof.contient("Demandes reçues", demande, "refusée")

	liste := nouveauDansLeMemeDossier(t, h)
	liste.State.Viewer = "prof"
	liste.Options.PublishIndex = false
	liste.Options.Requests = true
	liste.Options.ManageRequested = false
	if code := liste.muet(); code != app.ExitOK {
		t.Fatalf("liste : code = %d\n%s", code, liste.texte())
	}
	liste.contient(demande, "refusée", "la direction")
}

// La vue de comparaison n'a pas d'équivalent au terminal, mais la question
// qu'on s'y pose en premier — qui a remis d'abord — doit y trouver sa réponse,
// avec la même réserve qu'au navigateur.
func TestLOrdreDesRemisesEstDitSousLeTableauDesPaires(t *testing.T) {
	h := groupeRemis(t)
	h.Options.Handins = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("relevé : code = %d\n%s", code, h.texte())
	}

	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.Handins = false
	suivant.Options.Plagiarism = true
	suivant.Options.Yes = true
	if code := suivant.muet(); code != app.ExitOK {
		t.Fatalf("analyse : code = %d\n%s", code, suivant.texte())
	}

	// Jean-Luc a remis le premier, dix-neuf jours avant Émilie ; et la phrase
	// dit ce que cet ordre ne prouve pas.
	suivant.contient("Ordre des remises", "Jean-Luc Picard le 2026-09-01",
		"Émilie Côté le 2026-09-20", "pas qui a copié qui")
}

// La forme qu'une passe automatisée répète : sans travail nommé, les index
// annoncés qui manquent sont rattrapés, et l'état normal — il n'en manque
// aucun — se dit sans erreur.
func TestSansTravailNommeLesIndexManquantsSontRattrapes(t *testing.T) {
	h := groupeRemis(t)
	h.Options.PublishIndex = true
	h.Options.Manage = "a26.5n6.01.tp1"
	h.Options.ManageRequested = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("publication : code = %d\n%s", code, h.texte())
	}

	// Tout est publié : la passe le dit, et ne touche à rien.
	rien := nouveauDansLeMemeDossier(t, h)
	rien.Options.PublishIndex = true
	rien.Options.Manage = ""
	rien.Options.ManageRequested = false
	rien.Options.Yes = true
	if code := rien.muet(); code != app.ExitOK {
		t.Fatalf("rien à rattraper : code = %d\n%s", code, rien.texte())
	}
	rien.contient("Tous les travaux que vous avez annoncés ont leur index")

	// Le travail redevient annoncé sans index — ce que voit un collègue à qui
	// l'on n'a publié que le catalogue. La passe le rattrape sans qu'on ait à
	// le nommer.
	desindexer(t, h, "a26.5n6.01.tp1")
	passe := nouveauDansLeMemeDossier(t, h)
	passe.Options.PublishIndex = true
	passe.Options.Manage = ""
	passe.Options.ManageRequested = false
	passe.Options.Yes = true
	if code := passe.muet(); code != app.ExitOK {
		t.Fatalf("rattrapage : code = %d\n%s", code, passe.texte())
	}
	passe.contient("Index manquants", "a26.5n6.01.tp1", "1 index publié")

	// Et il ne manque plus rien : la passe est idempotente, ce qui est toute
	// la raison de la laisser tourner chaque semaine.
	apres := nouveauDansLeMemeDossier(t, h)
	apres.Options.PublishIndex = true
	apres.Options.Manage = ""
	apres.Options.ManageRequested = false
	apres.Options.Yes = true
	if code := apres.muet(); code != app.ExitOK {
		t.Fatalf("seconde passe : code = %d\n%s", code, apres.texte())
	}
	apres.contient("Tous les travaux que vous avez annoncés ont leur index")
}

// desindexer remet une ligne du catalogue dans l'état « annoncé, sans index ».
func desindexer(t *testing.T, h *harnais, travail string) {
	t.Helper()
	fichiers := h.State.Files("acme/.cohorte", "main")
	contenu, present := fichiers[exchange.CatalogFile]
	if !present {
		t.Fatalf("aucun catalogue publié : %v", fichiers)
	}
	var catalogue exchange.Catalog
	if err := json.Unmarshal([]byte(contenu), &catalogue); err != nil {
		t.Fatalf("catalogue : %v", err)
	}
	trouve := false
	for index := range catalogue.Teaching {
		if catalogue.Teaching[index].Key() == travail {
			catalogue.Teaching[index].Indexed = false
			trouve = true
		}
	}
	if !trouve {
		t.Fatalf("« %s » n'est pas au catalogue : %+v", travail, catalogue.Teaching)
	}
	payload, err := json.Marshal(catalogue)
	if err != nil {
		t.Fatalf("catalogue : %v", err)
	}
	fichiers[exchange.CatalogFile] = string(payload)
	h.State.SeedCommit("acme/.cohorte", fichiers, "main")
}

// Une marque délivrée à une inscrite sans compte se nomme au terminal comme au
// navigateur : par son matricule tant que personne n'a de compte, puis par son
// nom dès que le registre connaît un compte qui le porte. Elle s'écrivait
// « délivrée à @ », quand elle s'écrivait.
func TestUneMarqueDelivreeParMatriculeSeNommeAuTerminal(t *testing.T) {
	for _, cas := range []struct{ nom, etudiants, attendu string }{
		{"sans compte", `{"version": 2, "users": []}`,
			"délivrée au matricule 2100456 pour « a26.5n6.01.tp1 »"},
		{"compte rattaché", `{"version": 2, "users": [{"username": "nchery", ` +
			`"full_name": "Naomi Chéry", "student_id": "2100456", "is_teacher": false}]}`,
			"délivrée à Naomi Chéry pour « a26.5n6.01.tp1 »"},
	} {
		t.Run(cas.nom, func(t *testing.T) {
			h := groupeRemis(t)
			tirage, err := signature.New()
			if err != nil {
				t.Fatalf("tirage : %v", err)
			}
			// Naomi a reçu cette marque dans son README ; Émilie et Jean-Luc
			// l'ont recopié avec le reste.
			readme := string(signature.Sign([]byte("# Travail pratique 1\n"), tirage))
			for _, depot := range []string{
				"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard",
			} {
				h.State.SeedCommit("acme/"+depot, map[string]string{
					"src/Inventaire.java": gabaritTP, "src/Solution.java": solutionTP,
					"README.md": readme,
				}, "main")
			}
			h.State.AddRepo("acme", ".cohorte", true)
			h.State.SeedCommit("acme/.cohorte", map[string]string{
				"etudiants.json": cas.etudiants,
				"signatures.json": `{"version": 1, "issued": [{"assignment": ` +
					`"a26.5n6.01.tp1", "student_id": "2100456", "token": "` +
					signature.Text(tirage) + `"}]}`,
			}, "main")

			h.Options.Plagiarism = true
			h.Options.Manage = "a26.5n6.01.tp1"
			h.Options.ManageRequested = true
			h.Options.Yes = true
			if code := h.muet(); code != app.ExitOK {
				t.Fatalf("code = %d\n%s", code, h.texte())
			}
			h.contient("Même marque invisible", cas.attendu)
			h.absent("délivrée à @ ")
		})
	}
}
