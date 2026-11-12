package ui_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
)

// séparateur est celui que portent les suggestions quand la saisie emploie
// celui du système — la barre oblique inversée sous Windows.
const séparateur = string(filepath.Separator)

func console() (*ui.Console, *bytes.Buffer) {
	tampon := &bytes.Buffer{}
	return ui.NewConsoleFor(tampon), tampon
}

func TestSortieRedirigeeSansCouleurNiRetourChariot(t *testing.T) {
	c, tampon := console()
	if c.TTY || c.Color {
		t.Fatal("une sortie redirigée n'est ni un terminal ni colorée")
	}
	c.Heading("Titre")
	c.Success("créé %s", "tp1-a")
	c.Warning("attention")
	c.Failure("raté")
	c.Note("détail")
	c.Table([]string{"Dépôt", "Personne"}, [][]string{{"tp1-a", "Émilie Côté"}}, 0)

	sortie := tampon.String()
	if strings.Contains(sortie, "\x1b[") {
		t.Errorf("séquence d'échappement dans la sortie : %q", sortie)
	}
	if strings.Contains(sortie, "\r") {
		t.Errorf("retour chariot dans la sortie : %q", sortie)
	}
	if !strings.Contains(sortie, "créé tp1-a") || !strings.Contains(sortie, "Émilie Côté") {
		t.Errorf("sortie incomplète : %q", sortie)
	}
}

func TestTableAligneEtTronque(t *testing.T) {
	c, tampon := console()
	lignes := [][]string{
		{"1", "tp1-emilie-cote", "Émilie Côté"},
		{"2", "tp1-jlpicard", "Jean-Luc Picard"},
		{"3", "tp1-aminata-d", "Aminata Diallo"},
	}
	c.Table([]string{"#", "Dépôt", "Nom complet"}, lignes, 2)
	sortie := tampon.String()
	if !strings.Contains(sortie, "tp1-emilie-cote") || strings.Contains(sortie, "tp1-aminata-d") {
		t.Errorf("la limite n'est pas respectée : %q", sortie)
	}
	if !strings.Contains(sortie, "et 1 ligne de plus") {
		t.Errorf("les lignes masquées doivent être annoncées : %q", sortie)
	}
}

func TestProgressionHorsTerminal(t *testing.T) {
	c, tampon := console()
	barre := ui.NewProgress(c, "Comptes GitHub", 8)
	for done := 1; done <= 8; done++ {
		barre.Update(done, "@quelquun")
	}
	barre.Finish("  terminé")

	sortie := tampon.String()
	if strings.Contains(sortie, "\r") || strings.Contains(sortie, "\x1b[") {
		t.Errorf("la progression doit rester lisible dans un journal : %q", sortie)
	}
	if lignes := strings.Count(sortie, "Comptes GitHub"); lignes > 5 {
		t.Errorf("%d lignes de progression, c'est trop : %q", lignes, sortie)
	}
	if !strings.Contains(sortie, "8/8") {
		t.Errorf("la fin doit être annoncée : %q", sortie)
	}
}

func TestRoueMuetteHorsTerminal(t *testing.T) {
	c, tampon := console()
	passe := false
	ui.Await(c, "Vérification du jeton…", func() { passe = true })

	if !passe {
		t.Fatal("l'action doit se dérouler, terminal ou pas")
	}
	if sortie := tampon.String(); sortie != "" {
		t.Errorf("une animation n'a rien à faire dans un journal : %q", sortie)
	}
}

func TestRoueTourneEtSEfface(t *testing.T) {
	c, tampon := console()
	// La roue ne s'anime que sur un terminal ; le tampon en tient lieu, ce qui
	// permet d'examiner ce qu'elle y aurait écrit.
	c.TTY = true

	roue := ui.NewSpinner(c, "Lecture de l'organisation acme…")
	roue.Start()
	roue.Detail("42 lus")
	time.Sleep(400 * time.Millisecond)
	roue.Stop()

	sortie := tampon.String()
	if !strings.Contains(sortie, "Lecture de l'organisation acme…") {
		t.Errorf("la roue doit dire ce qu'on attend : %q", sortie)
	}
	if !strings.Contains(sortie, "42 lus") {
		t.Errorf("le détail doit suivre l'attente : %q", sortie)
	}
	if strings.Contains(sortie, "\n") {
		t.Errorf("la roue tient sur une ligne : %q", sortie)
	}
	// La ligne appartient à ce qui vient ensuite : elle doit être rendue vide.
	if !strings.HasSuffix(sortie, "\r\x1b[K") {
		t.Errorf("la roue doit effacer sa ligne en partant : %q", sortie)
	}
}

func TestRoueSArreteDeuxFoisSansBroncher(t *testing.T) {
	c, _ := console()
	c.TTY = true
	roue := ui.NewSpinner(c, "Attente")
	roue.Start()
	roue.Stop()
	roue.Stop()
	// Un Stop sans Start ne doit pas davantage bloquer : l'arrêt vient parfois
	// d'un chemin d'erreur qui n'a rien lancé.
	ui.NewSpinner(c, "Jamais lancée").Stop()
}

func TestScriptedAsk(t *testing.T) {
	scripte := ui.NewScripted("acme", "", "  tp1  ")
	valeur, err := scripte.Ask(ui.Question{Title: "Organisation"})
	if err != nil || valeur != "acme" {
		t.Fatalf("Ask = %q, %v", valeur, err)
	}
	valeur, err = scripte.Ask(ui.Question{Title: "Travail", Default: "tp0"})
	if err != nil || valeur != "tp0" {
		t.Fatalf("la réponse vide doit reprendre le défaut : %q, %v", valeur, err)
	}
	valeur, err = scripte.Ask(ui.Question{Title: "Travail", Validate: func(v string) (string, error) {
		return strings.ToUpper(v), nil
	}})
	if err != nil || valeur != "TP1" {
		t.Fatalf("Ask = %q, %v", valeur, err)
	}
	if _, err := scripte.Ask(ui.Question{Title: "De trop"}); err == nil {
		t.Error("un scénario épuisé doit échouer")
	}
}

func TestScriptedChoixEtConfirmation(t *testing.T) {
	options := ui.Options("creer", "Créer des dépôts", "gerer", "Gérer un groupe")
	scripte := ui.NewScripted("gerer", "2", "Créer", "", "oui", "non")

	for _, attendu := range []string{"gerer", "gerer", "creer", "creer"} {
		choix, err := scripte.Choose("Que faire ?", options, "creer")
		if err != nil || choix != attendu {
			t.Fatalf("Choose = %q, %v (attendu %q)", choix, err, attendu)
		}
	}
	if reponse, err := scripte.Confirm("Continuer ?", false); err != nil || !reponse {
		t.Errorf("Confirm = %v, %v", reponse, err)
	}
	if reponse, err := scripte.Confirm("Continuer ?", true); err != nil || reponse {
		t.Errorf("Confirm = %v, %v", reponse, err)
	}
}

func TestScriptedMultiSelect(t *testing.T) {
	options := ui.Options("a", "tp1-a", "b", "tp1-b", "c", "tp1-c")
	scripte := ui.NewScripted("tous", "1,3", "tp1-b", "-", "9")

	if choix, err := scripte.MultiSelect("Dépôts", options, nil); err != nil || len(choix) != 3 {
		t.Fatalf("« tous » = %v, %v", choix, err)
	}
	if choix, err := scripte.MultiSelect("Dépôts", options, nil); err != nil ||
		len(choix) != 2 || choix[0] != 0 || choix[1] != 2 {
		t.Fatalf("« 1,3 » = %v, %v", choix, err)
	}
	if choix, err := scripte.MultiSelect("Dépôts", options, nil); err != nil ||
		len(choix) != 1 || choix[0] != 1 {
		t.Fatalf("par nom = %v, %v", choix, err)
	}
	if choix, err := scripte.MultiSelect("Dépôts", options, nil); err != nil || len(choix) != 0 {
		t.Fatalf("« - » = %v, %v", choix, err)
	}
	if _, err := scripte.MultiSelect("Dépôts", options, nil); err == nil {
		t.Error("une sélection hors liste doit échouer")
	}
}

func TestScriptedInterruption(t *testing.T) {
	scripte := ui.NewScripted("\x03")
	if _, err := scripte.Ask(ui.Question{Title: "Organisation"}); err != ui.ErrAborted {
		t.Errorf("erreur = %v", err)
	}
}

func TestScriptPrompterRefuseToutesLesQuestions(t *testing.T) {
	prompteur := &ui.ScriptPrompter{}
	if prompteur.Interactive() {
		t.Error("le mode script n'est pas interactif")
	}
	if _, err := prompteur.Ask(ui.Question{Title: "Organisation"}); err == nil {
		t.Error("Ask doit échouer")
	} else if !strings.Contains(err.Error(), "non interactif") {
		t.Errorf("message = %v", err)
	}
	if _, err := prompteur.Confirm("Créer ?", false); err == nil {
		t.Error("Confirm doit échouer")
	} else if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("le message doit indiquer la correction : %v", err)
	}
	if _, err := prompteur.Choose("Quoi ?", nil, ""); err == nil {
		t.Error("Choose doit échouer")
	}
	if _, err := prompteur.MultiSelect("Quoi ?", nil, nil); err == nil {
		t.Error("MultiSelect doit échouer")
	}
}

func TestTableSansLigne(t *testing.T) {
	c, tampon := console()
	c.Table([]string{"Dépôt"}, nil, 10)
	if tampon.Len() != 0 {
		t.Errorf("un tableau vide ne doit rien écrire : %q", tampon.String())
	}
}

func TestBanniereEtTitres(t *testing.T) {
	c, tampon := console()
	c.Banner("gh milou 1.0", "Un dépôt par personne")
	c.Heading("Organisation cible")
	sortie := tampon.String()
	if !strings.Contains(sortie, "gh milou 1.0") || !strings.Contains(sortie, "Organisation cible") {
		t.Errorf("sortie = %q", sortie)
	}
}

func TestLinePrompterAsk(t *testing.T) {
	c, tampon := console()
	p := ui.NewLinePrompter(c, strings.NewReader("\nacme\n"))

	valeur, err := p.Ask(ui.Question{Title: "Organisation", Default: "defaut"})
	if err != nil || valeur != "defaut" {
		t.Fatalf("Ask = %q, %v", valeur, err)
	}
	valeur, err = p.Ask(ui.Question{Title: "Organisation"})
	if err != nil || valeur != "acme" {
		t.Fatalf("Ask = %q, %v", valeur, err)
	}
	if !strings.Contains(tampon.String(), "Organisation [defaut] :") {
		t.Errorf("invite = %q", tampon.String())
	}
}

func TestLinePrompterAskRedemandeApresErreur(t *testing.T) {
	c, tampon := console()
	p := ui.NewLinePrompter(c, strings.NewReader("-invalide-\noctocat\n"))
	valeur, err := p.Ask(ui.Question{
		Title: "Compte GitHub",
		Validate: func(value string) (string, error) {
			if strings.HasPrefix(value, "-") {
				return "", ui.ErrAborted
			}
			return value, nil
		},
	})
	if err != nil || valeur != "octocat" {
		t.Fatalf("Ask = %q, %v", valeur, err)
	}
	if !strings.Contains(tampon.String(), "✗") {
		t.Errorf("l'erreur doit être signalée : %q", tampon.String())
	}
}

func TestLinePrompterConfirm(t *testing.T) {
	c, _ := console()
	p := ui.NewLinePrompter(c, strings.NewReader("\no\nnon\npeut-être\noui\n"))

	if reponse, err := p.Confirm("Continuer ?", true); err != nil || !reponse {
		t.Fatalf("défaut = %v, %v", reponse, err)
	}
	if reponse, err := p.Confirm("Continuer ?", false); err != nil || !reponse {
		t.Fatalf("« o » = %v, %v", reponse, err)
	}
	if reponse, err := p.Confirm("Continuer ?", true); err != nil || reponse {
		t.Fatalf("« non » = %v, %v", reponse, err)
	}
	// Une réponse incomprise fait reposer la question.
	if reponse, err := p.Confirm("Continuer ?", false); err != nil || !reponse {
		t.Fatalf("après reprise = %v, %v", reponse, err)
	}
}

func TestLinePrompterChoose(t *testing.T) {
	c, tampon := console()
	options := ui.Options("creer", "Créer des dépôts", "gerer", "Gérer un groupe")
	p := ui.NewLinePrompter(c, strings.NewReader("2\n\n9\ngerer\n"))

	if choix, err := p.Choose("Que faire ?", options, "creer"); err != nil || choix != "gerer" {
		t.Fatalf("par numéro = %q, %v", choix, err)
	}
	if choix, err := p.Choose("Que faire ?", options, "creer"); err != nil || choix != "creer" {
		t.Fatalf("par défaut = %q, %v", choix, err)
	}
	// « 9 » sort de la liste : la question est reposée.
	if choix, err := p.Choose("Que faire ?", options, "creer"); err != nil || choix != "gerer" {
		t.Fatalf("après reprise = %q, %v", choix, err)
	}
	sortie := tampon.String()
	if !strings.Contains(sortie, "1  Créer des dépôts") || !strings.Contains(sortie, "2  Gérer un groupe") {
		t.Errorf("liste numérotée attendue : %q", sortie)
	}
	if strings.Contains(sortie, "\x1b[") {
		t.Errorf("aucune séquence d'échappement hors terminal : %q", sortie)
	}
}

func TestLinePrompterMultiSelect(t *testing.T) {
	c, _ := console()
	options := ui.Options("a", "tp1-a", "b", "tp1-b", "c", "tp1-c")
	p := ui.NewLinePrompter(c, strings.NewReader("\n1,3\ntp1-b\n42\n2-3\n-\n"))

	cas := [][]int{{0, 1, 2}, {0, 2}, {1}, {1, 2}}
	for _, attendu := range cas {
		indices, err := p.MultiSelect("Dépôts", options, nil)
		if err != nil {
			t.Fatalf("MultiSelect : %v", err)
		}
		if len(indices) != len(attendu) {
			t.Fatalf("MultiSelect = %v, attendu %v", indices, attendu)
		}
		for position, valeur := range attendu {
			if indices[position] != valeur {
				t.Fatalf("MultiSelect = %v, attendu %v", indices, attendu)
			}
		}
	}
	if indices, err := p.MultiSelect("Dépôts", options, nil); err != nil || len(indices) != 0 {
		t.Errorf("« - » = %v, %v", indices, err)
	}
}

func TestLinePrompterFinDeFlux(t *testing.T) {
	c, _ := console()
	p := ui.NewLinePrompter(c, strings.NewReader(""))
	if _, err := p.Ask(ui.Question{Title: "Organisation"}); err != ui.ErrAborted {
		t.Errorf("erreur = %v", err)
	}
}

// dossierDEssai prépare une arborescence pour les tests de complétion.
func dossierDEssai(t *testing.T) string {
	t.Helper()
	racine := t.TempDir()
	for _, nom := range []string{"cohorte.csv", "cohorte-hiver.csv"} {
		if err := os.WriteFile(filepath.Join(racine, nom), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(racine, "depart"), 0o755); err != nil {
		t.Fatal(err)
	}
	complete.Forget()
	return racine
}

func TestLinePrompterCompletionUneSeuleCorrespondance(t *testing.T) {
	racine := dossierDEssai(t)
	c, _ := console()
	// La tabulation demande la complétion, la ligne suivante accepte le résultat.
	entree := filepath.Join(racine, "dep") + "\t\n\n"
	p := ui.NewLinePrompter(c, strings.NewReader(entree))

	valeur, err := p.Ask(ui.Question{Title: "Dossier de départ", Complete: complete.Dir})
	if err != nil {
		t.Fatalf("Ask : %v", err)
	}
	if valeur != filepath.Join(racine, "depart")+séparateur {
		t.Errorf("valeur = %q", valeur)
	}
}

func TestLinePrompterCompletionPlusieursCorrespondances(t *testing.T) {
	racine := dossierDEssai(t)
	c, tampon := console()
	entree := filepath.Join(racine, "coh") + "\t\n\n"
	p := ui.NewLinePrompter(c, strings.NewReader(entree))

	valeur, err := p.Ask(ui.Question{Title: "Fichier CSV", Complete: complete.Path})
	if err != nil {
		t.Fatalf("Ask : %v", err)
	}
	// Une seule tabulation acquiert la partie commune, sans encore rien lister.
	if valeur != filepath.Join(racine, "cohorte") {
		t.Errorf("valeur = %q", valeur)
	}
	if strings.Contains(tampon.String(), "cohorte-hiver.csv") {
		t.Errorf("la liste doit attendre la tabulation suivante :\n%s", tampon.String())
	}
}

func TestLinePrompterDoubleTabulationListe(t *testing.T) {
	racine := dossierDEssai(t)
	c, tampon := console()
	// Deux tabulations dans la même saisie : les possibilités s'affichent.
	entree := filepath.Join(racine, "coh") + "\t\t\n\n"
	p := ui.NewLinePrompter(c, strings.NewReader(entree))

	if _, err := p.Ask(ui.Question{Title: "Fichier CSV", Complete: complete.Path}); err != nil {
		t.Fatalf("Ask : %v", err)
	}
	sortie := tampon.String()
	if !strings.Contains(sortie, "cohorte.csv") || !strings.Contains(sortie, "cohorte-hiver.csv") {
		t.Errorf("les possibilités doivent être listées :\n%s", sortie)
	}
}

func TestLinePrompterDoubleTabulationListeLeRepertoireCourant(t *testing.T) {
	racine := dossierDEssai(t)
	precedent, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(racine); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(precedent)
	complete.Forget()

	c, tampon := console()
	p := ui.NewLinePrompter(c, strings.NewReader("\t\t\ncohorte.csv\n"))
	if _, err := p.Ask(ui.Question{Title: "Fichier CSV", Complete: complete.Path}); err != nil {
		t.Fatalf("Ask : %v", err)
	}
	sortie := tampon.String()
	for _, attendu := range []string{"cohorte.csv", "depart" + séparateur} {
		if !strings.Contains(sortie, attendu) {
			t.Errorf("« %s » absent :\n%s", attendu, sortie)
		}
	}
}

func TestLinePrompterCompletionSansCorrespondance(t *testing.T) {
	racine := dossierDEssai(t)
	c, tampon := console()
	entree := filepath.Join(racine, "zzz") + "\t\n" + filepath.Join(racine, "cohorte.csv") + "\n"
	p := ui.NewLinePrompter(c, strings.NewReader(entree))

	valeur, err := p.Ask(ui.Question{Title: "Fichier CSV", Complete: complete.Path})
	if err != nil {
		t.Fatalf("Ask : %v", err)
	}
	if valeur != filepath.Join(racine, "cohorte.csv") {
		t.Errorf("valeur = %q", valeur)
	}
	if !strings.Contains(tampon.String(), "Aucune correspondance") {
		t.Errorf("sortie = %s", tampon.String())
	}
}

func TestLinePrompterTabulationIgnoreeSansCompletion(t *testing.T) {
	c, _ := console()
	p := ui.NewLinePrompter(c, strings.NewReader("valeur\tavec tabulation\n"))
	valeur, err := p.Ask(ui.Question{Title: "Question ordinaire"})
	if err != nil {
		t.Fatalf("Ask : %v", err)
	}
	if valeur != "valeur\tavec tabulation" {
		t.Errorf("valeur = %q", valeur)
	}
}

func TestLinePrompterCompletionDepuisLeDefaut(t *testing.T) {
	racine := dossierDEssai(t)
	c, _ := console()
	// Une tabulation seule complète à partir de la valeur proposée par défaut.
	p := ui.NewLinePrompter(c, strings.NewReader("\t\n\n"))
	valeur, err := p.Ask(ui.Question{
		Title:    "Dossier de départ",
		Default:  filepath.Join(racine, "dep"),
		Complete: complete.Dir,
	})
	if err != nil {
		t.Fatalf("Ask : %v", err)
	}
	if valeur != filepath.Join(racine, "depart")+séparateur {
		t.Errorf("valeur = %q", valeur)
	}
}

func TestLinePrompterTabulationApresAvoirAtteintUnDossier(t *testing.T) {
	racine := dossierDEssai(t)
	// Le dossier atteint doit contenir de quoi lister.
	for _, nom := range []string{"seance-1.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(racine, "depart", nom), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	complete.Forget()

	c, tampon := console()
	// Une tabulation atteint le dossier, la suivante liste son contenu.
	entree := filepath.Join(racine, "dep") + "\t\n\t\n\n"
	p := ui.NewLinePrompter(c, strings.NewReader(entree))

	if _, err := p.Ask(ui.Question{Title: "Dossier", Complete: complete.Path}); err != nil {
		t.Fatalf("Ask : %v", err)
	}
	sortie := tampon.String()
	for _, attendu := range []string{"seance-1.md", "notes.txt"} {
		if !strings.Contains(sortie, attendu) {
			t.Errorf("« %s » absent :\n%s", attendu, sortie)
		}
	}
}
