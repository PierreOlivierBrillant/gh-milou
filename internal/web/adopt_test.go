package web_test

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// ------------------------------------------------------ adoption par gabarit

// ---------------------------------------------------- déplacer un étudiant

func TestEtudiantDeplaceAvecSesDepots(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.travailsession.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01",
		"Jean-Luc Picard", "jlpicard", "Émilie Côté", "emilie-cote")
	arrivee := h.groupe("a26", "5n6", "02", "Aminata Diallo", "aminata-d")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(2) || resultat["failed"] != float64(0) {
		t.Fatalf("bilan : %+v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote," +
		"a26.5n6.02.tp1.jean-luc-picard,a26.5n6.02.travailsession.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	// Les deux listes ont suivi.
	var avant, apres struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+depart, nil, &avant)
	h.json(http.MethodGet, "/api/classrooms/"+arrivee, nil, &apres)
	if len(avant.Students) != 1 || avant.Students[0].Username != "emilie-cote" {
		t.Fatalf("groupe de départ : %+v", avant.Students)
	}
	if len(apres.Students) != 2 {
		t.Fatalf("groupe d'arrivée : %+v", apres.Students)
	}
}

// Une personne qui n'a encore aucun dépôt n'existe que dans la liste : il n'y a
// rien à renommer, et la réponse vient tout de suite.
func TestEtudiantSansDepotDeplaceSaListeSeule(t *testing.T) {
	h := nouveau(t, nil)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	arrivee := h.groupe("a26", "5n6", "02", "Aminata Diallo", "aminata-d")

	var bilan struct {
		Moved   []string `json:"moved"`
		Count   int      `json:"count"`
		Renamed int      `json:"renamed"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee}, &bilan)
	if bilan.Count != 1 || bilan.Moved[0] != "jlpicard" || bilan.Renamed != 0 {
		t.Fatalf("bilan : %+v", bilan)
	}
}

// Le nom d'un dépôt dit à quel groupe il appartient : déplacer quelqu'un sans
// l'emporter le montrerait des deux côtés — dans la liste de l'arrivée, et dans
// le groupe de départ que ses dépôts lui rattachent encore. Il n'y a donc rien
// à demander : les dépôts suivent toujours.
func TestDeplacementEmporteToujoursLesDepots(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	arrivee := h.groupe("a26", "5n6", "02", "Aminata Diallo", "aminata-d")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	if noms := h.depots(); len(noms) != 1 ||
		noms[0] != "a26.5n6.02.tp1.jean-luc-picard" {
		t.Fatalf("dépôts : %v", noms)
	}
}

// Les listes n'écoutent que GitHub : un renommage qui échoue les laisse où
// elles sont, plutôt que d'affirmer sur cette machine un rangement que
// l'organisation contredit.
func TestListesNeSuiventPasUnRenommageEchoue(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.travailsession.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	state.FailOn["PATCH /repos/acme/a26.5n6.01.travailsession.jean-luc-picard"] =
		fakegh.Failure{Status: 403, Message: "Forbidden"}

	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	arrivee := h.groupe("a26", "5n6", "02", "Aminata Diallo", "aminata-d")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["failed"] != float64(1) || resultat["count"] != float64(0) {
		t.Fatalf("bilan : %+v", resultat)
	}

	// Le fichier local est relu directement : l'interface, elle, montre déjà
	// l'arrivée que le dépôt renommé lui rattache — et c'est ce qu'elle doit
	// faire, puisque GitHub le dit.
	if declares := h.declares(depart); len(declares) != 1 || declares[0] != "jlpicard" {
		t.Fatalf("le groupe de départ a perdu quelqu'un : %v", declares)
	}
	if declares := h.declares(arrivee); len(declares) != 1 || declares[0] != "aminata-d" {
		t.Fatalf("le groupe d'arrivée a gagné quelqu'un : %v", declares)
	}
}

func TestPlusieursEtudiantsDeplacesEnsemble(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.tp1.aminata-diallo",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard",
		"Émilie Côté", "emilie-cote", "Aminata Diallo", "aminata-d")
	arrivee := h.groupe("a26", "5n6", "02")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{
			"usernames": []string{"jlpicard", "aminata-d"},
			"target":    arrivee,
		})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["count"] != float64(2) || resultat["renamed"] != float64(2) {
		t.Fatalf("bilan : %+v", resultat)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.emilie-cote," +
		"a26.5n6.02.tp1.aminata-diallo,a26.5n6.02.tp1.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	var reste struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+depart, nil, &reste)
	if len(reste.Students) != 1 || reste.Students[0].Username != "emilie-cote" {
		t.Fatalf("groupe de départ : %+v", reste.Students)
	}
}

// Une inscrite que seul son matricule désigne se déplace comme les autres : sa
// ligne envoie son matricule, ses dépôts suivent, elle quitte la liste de
// départ et figure une fois dans celle d'arrivée. Claire, sans compte elle
// aussi, reste où elle est, avec son dépôt.
func TestDeplacerQuiNAQueSonMatricule(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.nadia-pepin",
		"a26.5n6.01.tp1.claire-otis",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	h.poserListe(depart, inscritParCompte("Jean-Luc Picard", "jlpicard"),
		inscritParMatricule("Nadia Pépin", "2100110"),
		inscritParMatricule("Claire Otis", "2100789"))
	arrivee := h.groupe("a26", "5n6", "02", "Aminata Diallo", "aminata-d")

	// Le dialogue compte les dépôts que la ligne lui montre : sans eux, il
	// annonçait n'en renommer aucun, puis les renommait.
	if nadia := h.ligneDe(depart, "Nadia Pépin"); len(nadia.Assignments) != 1 {
		t.Errorf("la ligne de Nadia doit montrer son dépôt : %+v", nadia)
	}

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"usernames": []string{"2100110"}, "target": arrivee})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	if bilan["label"] != "Déplacement de Nadia Pépin vers « Groupe 02 »" {
		t.Errorf("titre du journal : %v", bilan["label"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["count"] != float64(1) || resultat["renamed"] != float64(1) {
		t.Fatalf("bilan : %+v", resultat)
	}
	if designees := fmt.Sprint(resultat["moved"]); designees != "[2100110]" {
		t.Errorf("le bilan doit désigner Nadia par son matricule : %s", designees)
	}

	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.01.tp1.claire-otis,a26.5n6.01.tp1.jean-luc-picard," +
		"a26.5n6.02.tp1.nadia-pepin"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	if reste := nomsDe(h.liste(depart)); reste != "Claire Otis, Jean-Luc Picard" {
		t.Errorf("groupe de départ : %s", reste)
	}
	if arrives := nomsDe(h.liste(arrivee)); arrives != "Aminata Diallo, Nadia Pépin" {
		t.Errorf("groupe d'arrivée : %s", arrives)
	}
	if nadia := h.ligneDe(arrivee, "Nadia Pépin"); nadia.StudentID != "2100110" ||
		len(nadia.Assignments) != 1 || nadia.Assignments[0].Repo != "a26.5n6.02.tp1.nadia-pepin" {
		t.Errorf("ligne de Nadia à l'arrivée : %+v", nadia)
	}
}

// ligneDeListe est ce que la liste d'un groupe dit d'une personne au navigateur.
type ligneDeListe struct {
	FullName    string `json:"full_name"`
	Username    string `json:"username"`
	StudentID   string `json:"student_id"`
	Assignments []struct {
		Repo string `json:"repo"`
	} `json:"assignments"`
}

// liste rend les lignes de la liste d'un groupe, dans l'ordre où le navigateur
// les reçoit : par nom.
func (h *harnais) liste(place string) []ligneDeListe {
	h.t.Helper()
	var reponse struct {
		Students []ligneDeListe `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/"+place+"/students", nil, &reponse)
	return reponse.Students
}

// ligneDe rend la ligne d'une personne, retrouvée par son nom.
func (h *harnais) ligneDe(place, nom string) ligneDeListe {
	h.t.Helper()
	for _, ligne := range h.liste(place) {
		if ligne.FullName == nom {
			return ligne
		}
	}
	h.t.Fatalf("%s n'est pas dans la liste de %s", nom, place)
	return ligneDeListe{}
}

// nomsDe rend les noms d'une liste, dans son ordre.
func nomsDe(lignes []ligneDeListe) string {
	noms := make([]string, 0, len(lignes))
	for _, ligne := range lignes {
		noms = append(noms, ligne.FullName)
	}
	return strings.Join(noms, ", ")
}

// Une erreur d'inscription laisse parfois quelqu'un avec des dépôts dans deux
// groupes à la fois. Le déplacement est ce qui les rassemble : figurer déjà
// dans la liste d'arrivée ne le refuse pas, sa fiche y fusionne avec celle qui
// l'attendait au lieu de s'y dédoubler.
func TestEtudiantDejaInscritALArriveeSyRassemble(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.1030.tp1.jean-luc-picard", "a26.5n6.1040.tp2.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "1040", "Jean-Luc Picard", "jlpicard")
	arrivee := h.groupe("a26", "5n6", "1030", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee})
	if bilan["status"] != "terminé" {
		t.Fatalf("travail %v : %v", bilan["status"], bilan["failure"])
	}
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["count"] != float64(1) || resultat["renamed"] != float64(1) {
		t.Fatalf("bilan : %+v", resultat)
	}

	// Les deux travaux sont sous le même toit, chacun sous son propre nom.
	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.1030.tp1.jean-luc-picard,a26.5n6.1030.tp2.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}

	if declares := h.declares(depart); len(declares) != 0 {
		t.Fatalf("groupe de départ : %v", declares)
	}
	if declares := h.declares(arrivee); len(declares) != 1 || declares[0] != "jlpicard" {
		t.Fatalf("groupe d'arrivée : %v", declares)
	}
}

// Deux dépôts ne peuvent pas porter le même nom : le même travail des deux
// côtés refuse le rassemblement, et le dit avant le premier renommage plutôt
// que de laisser la moitié du déplacement derrière.
func TestRassemblementRefuseQuandLeMemeTravailExisteDesDeuxCotes(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.1030.tp1.jean-luc-picard", "a26.5n6.1040.tp1.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "1040", "Jean-Luc Picard", "jlpicard")
	arrivee := h.groupe("a26", "5n6", "1030", "Jean-Luc Picard", "jlpicard")

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+depart+"/students/move",
		map[string]any{"username": "jlpicard", "target": arrivee})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "a26.5n6.1030.tp1.jean-luc-picard") {
		t.Fatalf("message : %s", contenu)
	}

	// Rien n'a bougé : ni les dépôts, ni les listes.
	noms := h.depots()
	sort.Strings(noms)
	attendu := "a26.5n6.1030.tp1.jean-luc-picard,a26.5n6.1040.tp1.jean-luc-picard"
	if strings.Join(noms, ",") != attendu {
		t.Fatalf("dépôts : %v", noms)
	}
	if declares := h.declares(depart); len(declares) != 1 {
		t.Fatalf("groupe de départ : %v", declares)
	}
}

// Le groupe d'arrivée n'a pas à exister d'avance : c'est le déplacement qui le
// déclare, sans qu'on ait à sortir de la liste des étudiants pour le créer.
func TestDeplacementDeclareLeGroupeDArrivee(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+depart+"/students/move",
		map[string]any{
			"usernames": []string{"jlpicard"},
			"new_group": map[string]string{"session": "a26", "course": "5n6", "group": "03"},
		})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["created"] != true || resultat["target_scope"] != "a26.5n6.03" {
		t.Fatalf("bilan : %+v", resultat)
	}
	if noms := h.depots(); len(noms) != 1 ||
		noms[0] != "a26.5n6.03.tp1.jean-luc-picard" {
		t.Fatalf("dépôts : %v", noms)
	}

	var neuf struct {
		Students []struct {
			Username string `json:"username"`
		} `json:"students"`
	}
	h.json(http.MethodGet, "/api/classrooms/a26.5n6.03", nil, &neuf)
	if len(neuf.Students) != 1 || neuf.Students[0].Username != "jlpicard" {
		t.Fatalf("groupe déclaré : %+v", neuf.Students)
	}
}

// Déclarer sur une place déjà occupée serait une fusion déguisée : elle est
// refusée, et la place se choisit alors dans la liste.
func TestNouveauGroupeSurUnePlaceOccupeeRefuse(t *testing.T) {
	h := nouveau(t, nil)
	depart := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	h.groupe("a26", "5n6", "02", "Émilie Côté", "emilie-cote")

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+depart+"/students/move", map[string]any{
			"usernames": []string{"jlpicard"},
			"new_group": map[string]string{"session": "a26", "course": "5n6", "group": "02"},
		})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), "existe déjà") {
		t.Fatalf("message : %s", contenu)
	}
}

// ------------------------------------------------- renommer un groupe déclaré

func TestGroupeDeclareSeRenomme(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	h := nouveau(t, state)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")

	var apercu struct {
		Scope  string `json:"scope"`
		Ready  int    `json:"ready"`
		Switch bool   `json:"switch"`
	}
	h.json(http.MethodPost, "/api/classrooms/"+id+"/migration/preview",
		map[string]any{"session": "h27", "course": "5n6", "group": "02"}, &apercu)
	if apercu.Scope != "h27.5n6.02" || apercu.Ready != 1 || !apercu.Switch {
		t.Fatalf("aperçu : %+v", apercu)
	}

	bilan := h.travail(http.MethodPost, "/api/classrooms/"+id+"/migration/apply",
		map[string]any{"session": "h27", "course": "5n6", "group": "02"})
	resultat, _ := bilan["result"].(map[string]any)
	if resultat["renamed"] != float64(1) || resultat["switched"] != true {
		t.Fatalf("bilan : %+v", resultat)
	}
	if noms := h.depots(); len(noms) != 1 ||
		noms[0] != "h27.5n6.02.tp1.jean-luc-picard" {
		t.Fatalf("dépôts : %v", noms)
	}
}

func TestGroupeDejaALaBonnePlaceRefuse(t *testing.T) {
	h := nouveau(t, nil)
	id := h.groupe("a26", "5n6", "01", "Jean-Luc Picard", "jlpicard")
	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/"+id+"/migration/preview",
		map[string]any{"session": "a26", "course": "5n6", "group": "01"})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
}

// ------------------------------------------------------------- chemins locaux

func TestExplorateurListeUnDossier(t *testing.T) {
	h := nouveau(t, nil)
	dossier := t.TempDir()

	var listing struct {
		Path    string `json:"path"`
		Parent  string `json:"parent"`
		Entries []struct {
			Name string `json:"name"`
			Dir  bool   `json:"dir"`
		} `json:"entries"`
	}
	h.json(http.MethodPost, "/api/paths/browse",
		map[string]any{"path": dossier, "dirs": false}, &listing)
	if listing.Path != dossier || listing.Parent == "" {
		t.Fatalf("listing : %+v", listing)
	}

	// Un chemin qui n'existe pas remonte à ce qui existe : ouvrir
	// l'explorateur ne doit jamais échouer sur un souvenir.
	h.json(http.MethodPost, "/api/paths/browse",
		map[string]any{"path": dossier + "/rien/du/tout", "dirs": true}, &listing)
	if listing.Path != dossier {
		t.Fatalf("repli : %+v", listing)
	}
}
