package web_test

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
)

// ficheRendu est ce que l'API rend pour une personne.
type ficheRendu struct {
	User struct {
		FullName  string   `json:"full_name"`
		Username  string   `json:"username"`
		Accounts  []string `json:"accounts"`
		IsTeacher bool     `json:"is_teacher"`
		Role      string   `json:"role"`
		Known     bool     `json:"known"`
		Repos     int      `json:"repos"`
		Courses   int      `json:"courses"`
		Taught    int      `json:"taught"`
		PushedAt  string   `json:"pushed_at"`
		Timeline  []struct {
			Scope       string `json:"scope"`
			SessionName string `json:"session_name"`
			Course      string `json:"course"`
			Group       string `json:"group"`
			Role        string `json:"role"`
			Silent      bool   `json:"silent"`
			PushedAt    string `json:"pushed_at"`
			Assignments []struct {
				Name string `json:"name"`
				Repo string `json:"repo"`
				URL  string `json:"url"`
			} `json:"assignments"`
		} `json:"timeline"`
	} `json:"user"`
	Viewer        string `json:"viewer"`
	ViewerTeaches bool   `json:"viewer_teaches"`
	Host          string `json:"host"`
}

func (h *harnais) fiche(compte string) ficheRendu {
	h.t.Helper()
	var rendu ficheRendu
	h.json(http.MethodGet, "/api/users/"+compte, nil, &rendu)
	return rendu
}

// chronologie abrège la lecture d'une fiche.
func chronologie(rendu ficheRendu) string {
	etapes := make([]string, 0, len(rendu.User.Timeline))
	for _, etape := range rendu.User.Timeline {
		etapes = append(etapes, etape.Scope+":"+etape.Role)
	}
	return strings.Join(etapes, ",")
}

// La fiche déroule le passage d'une personne, de la session la plus récente à
// la plus ancienne, avec les dépôts de chaque cours.
func TestLaFicheDerouleLePassageDUnePersonne(t *testing.T) {
	rendu := college(t).fiche("emilie-cote")

	if rendu.User.FullName != "Émilie Côté" {
		t.Fatalf("fiche = %+v", rendu.User)
	}
	if chronologie(rendu) != "h27.5n6.02:étudiant,a26.4w6.01:étudiant,a26.5n6.01:étudiant" {
		t.Fatalf("chronologie = %s", chronologie(rendu))
	}
	if rendu.User.Courses != 3 || rendu.User.Repos != 3 {
		t.Errorf("cours = %d, dépôts = %d", rendu.User.Courses, rendu.User.Repos)
	}
	// Chaque étape se lit sans traduction, et ses dépôts mènent à GitHub.
	tete := rendu.User.Timeline[0]
	if tete.SessionName == "" || tete.Group != "02" {
		t.Errorf("étape = %+v", tete)
	}
	if len(tete.Assignments) != 1 || tete.Assignments[0].Name != "tp1" {
		t.Fatalf("travaux = %+v", tete.Assignments)
	}
	if !strings.Contains(tete.Assignments[0].URL, "h27.5n6.02.tp1.emilie-cote") {
		t.Errorf("adresse du dépôt = %q", tete.Assignments[0].URL)
	}
	// La page compose les adresses de profils : l'hôte lui vient d'ici.
	if rendu.Host == "" {
		t.Error("la fiche doit dire l'hôte GitHub")
	}
}

// Un compte que rien ne connaît rend une fiche vide plutôt qu'une page absente.
func TestLaFicheDUnCompteInconnuResteUnePage(t *testing.T) {
	rendu := college(t).fiche("personne-du-tout")
	if rendu.User.Known || len(rendu.User.Timeline) != 0 {
		t.Fatalf("fiche = %+v", rendu.User)
	}
	if rendu.User.Username != "personne-du-tout" {
		t.Errorf("compte = %q", rendu.User.Username)
	}
}

// --------------------------------------------------------------- cooptation

// coopter abrège la reconnaissance d'un enseignant.
func (h *harnais) coopter(compte string, enseignant bool) (*http.Response, []byte) {
	h.t.Helper()
	return h.requete(http.MethodPost, "/api/users/"+compte+"/role",
		map[string]any{"is_teacher": enseignant})
}

// Tant que l'organisation n'a aucun enseignant, quelqu'un doit pouvoir
// commencer : celui qui a le droit d'écrire dans « .cohorte » est justement
// celui à qui ce droit a été donné.
func TestLePremierEnseignantSeDeclare(t *testing.T) {
	h := college(t)

	reponse, contenu := h.coopter("emilie-cote", true)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}
	if !strings.Contains(string(contenu), `"is_teacher":true`) {
		t.Fatalf("réponse = %s", contenu)
	}
	if fiche := h.fiche("emilie-cote"); !fiche.User.IsTeacher || fiche.User.Role != "enseignant" {
		t.Errorf("fiche = %+v", fiche.User)
	}
}

// Un étudiant ne coopte pas. Ce n'est pas cette vérification qui protège le
// registre — GitHub lui refuserait l'écriture bien avant —, mais elle dit le
// refus dans les mots de l'outil.
func TestUnEtudiantNeCoopte(t *testing.T) {
	h := college(t)
	// « prof » regarde, et l'organisation a déjà un enseignant qui n'est pas lui.
	if reponse, contenu := h.coopter("emilie-cote", true); reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut %d — %s", reponse.StatusCode, contenu)
	}

	reponse, contenu := h.coopter("jlpicard", true)
	if reponse.StatusCode == http.StatusOK {
		t.Fatal("un compte non enseignant ne doit pas pouvoir coopter")
	}
	if !strings.Contains(string(contenu), "Seul un enseignant") {
		t.Errorf("refus = %s", contenu)
	}
}

// Retirer le dernier rôle d'enseignant laisserait l'organisation sans personne
// pour en reconnaître un autre : plus aucune fiche ne pourrait rendre le sien.
func TestLeDernierEnseignantNeSeRetirePas(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)

	reponse, contenu := h.coopter("prof", false)
	if reponse.StatusCode == http.StatusOK {
		t.Fatal("retirer le dernier enseignant doit être refusé")
	}
	if !strings.Contains(string(contenu), "seul enseignant") {
		t.Errorf("refus = %s", contenu)
	}
}

// ------------------------------------------------------------ cloisonnement

// cloisonnementRendu est l'état de l'équipe enseignante d'un groupe.
type cloisonnementRendu struct {
	Scope string `json:"scope"`
	State struct {
		Name     string `json:"name"`
		Slug     string `json:"slug"`
		Exists   bool   `json:"exists"`
		Repos    int    `json:"repos"`
		Teachers []struct {
			FullName string `json:"full_name"`
			Username string `json:"username"`
		} `json:"teachers"`
	} `json:"state"`
	Candidates []struct {
		Username string `json:"username"`
		Member   bool   `json:"member"`
	} `json:"candidates"`
	Notice string `json:"notice"`
}

func (h *harnais) cloisonnement(scope string) cloisonnementRendu {
	h.t.Helper()
	var rendu cloisonnementRendu
	h.json(http.MethodGet, "/api/classrooms/"+scope+"/teachers", nil, &rendu)
	return rendu
}

// Cloisonner un groupe crée son équipe enseignante et lui donne ses dépôts.
// Elle n'est pas une équipe du groupe : les équipes d'étudiants l'ignorent.
func TestCloisonnerUnGroupeLuiDonneSonEquipe(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)

	avant := h.cloisonnement("a26.5n6.01")
	if avant.State.Exists {
		t.Fatal("le groupe ne doit pas être cloisonné d'avance")
	}
	if avant.State.Name != "a26.5n6.01.enseignants" || avant.State.Repos != 2 {
		t.Fatalf("état = %+v", avant.State)
	}
	// L'équipe ouvre un accès sans en fermer aucun : le dire est le minimum.
	if avant.Notice == "" {
		t.Error("l'état doit dire ce que l'équipe ne ferme pas")
	}
	if len(avant.Candidates) != 1 || avant.Candidates[0].Username != "prof" {
		t.Fatalf("candidats = %+v", avant.Candidates)
	}

	etat := h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/teachers",
		map[string]any{"teachers": []string{"prof"}})
	if etat["status"] != "terminé" {
		t.Fatalf("cloisonnement = %+v", etat)
	}

	apres := h.cloisonnement("a26.5n6.01")
	if !apres.State.Exists || len(apres.State.Teachers) != 1 {
		t.Fatalf("état = %+v", apres.State)
	}
	if apres.State.Teachers[0].Username != "prof" {
		t.Errorf("enseignants = %+v", apres.State.Teachers)
	}
	// Les dépôts du groupe sont allés à l'équipe — les siens seulement.
	partages := h.State.TeamRepos["acme/"+apres.State.Slug]
	if len(partages) != 2 {
		t.Fatalf("dépôts partagés = %v", partages)
	}
	for nom, droit := range partages {
		if !strings.HasPrefix(nom, "acme/a26.5n6.01.") {
			t.Errorf("dépôt hors du groupe : %s", nom)
		}
		if droit != "admin" {
			t.Errorf("%s : droit = %q", nom, droit)
		}
	}
}

// On n'inscrit à l'équipe d'un groupe que quelqu'un que le registre déclare
// enseignant : « is_teacher » ne donne rien, il autorise à donner.
func TestOnNeCloisonnePasDerriereUnEtudiant(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/a26.5n6.01/teachers/preview",
		map[string]any{"teachers": []string{"prof", "emilie-cote"}})
	if reponse.StatusCode == http.StatusOK {
		t.Fatal("inscrire un étudiant à l'équipe enseignante doit être refusé")
	}
	if !strings.Contains(string(contenu), "emilie-cote") {
		t.Errorf("refus = %s", contenu)
	}
}

// S'exclure soi-même du groupe qu'on cloisonne ferait perdre l'accès sans
// aucun chemin pour y revenir.
func TestOnNeSExclutPasDuGroupeQuOnCloisonne(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)
	h.coopter("emilie-cote", true)

	reponse, contenu := h.requete(http.MethodPost,
		"/api/classrooms/a26.5n6.01/teachers/preview",
		map[string]any{"teachers": []string{"emilie-cote"}})
	if reponse.StatusCode == http.StatusOK {
		t.Fatal("s'exclure du groupe qu'on cloisonne doit être refusé")
	}
	if !strings.Contains(string(contenu), "sans pouvoir y revenir") {
		t.Errorf("refus = %s", contenu)
	}
}

// Un cours donné figure dans la chronologie de celui qui l'a donné : c'est ce
// qui permet de chercher ce qu'un collègue a déjà enseigné.
func TestUnCoursDonneFigureDansLaFiche(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/teachers",
		map[string]any{"teachers": []string{"prof"}})

	fiche := h.fiche("prof")
	if chronologie(fiche) != "a26.5n6.01:enseignant" {
		t.Fatalf("chronologie = %s", chronologie(fiche))
	}
	if fiche.User.Taught != 1 || fiche.User.Courses != 0 {
		t.Errorf("donnés = %d, suivis = %d", fiche.User.Taught, fiche.User.Courses)
	}
	if !fiche.ViewerTeaches {
		t.Error("celui qui regarde enseigne : la page doit le savoir")
	}
}

// ------------------------------------------------------------- nommer

// Le nom vit au registre, pas dans un groupe : le donner depuis la fiche vaut
// pour tous les cours de la personne.
func TestNommerDepuisLaFiche(t *testing.T) {
	h := college(t)
	h.sansNoms("h27", "5n6", "02", "aleksilesaj")

	avant := h.fiche("aleksilesaj")
	if avant.User.FullName != "" {
		t.Fatalf("nom = %q", avant.User.FullName)
	}

	var rendu struct {
		Username string `json:"username"`
		FullName string `json:"full_name"`
	}
	h.json(http.MethodPut, "/api/users/aleksilesaj/name",
		map[string]any{"full_name": "Aleksi Lesaj"}, &rendu)
	if rendu.FullName != "Aleksi Lesaj" {
		t.Fatalf("réponse = %+v", rendu)
	}

	apres := h.fiche("aleksilesaj")
	if apres.User.FullName != "Aleksi Lesaj" || !apres.User.Known {
		t.Fatalf("fiche = %+v", apres.User)
	}
	// Le nom vaut partout : l'annuaire le montre sans qu'on ait rien publié.
	for _, ligne := range h.annuaire("").Users {
		if ligne.Username == "aleksilesaj" && ligne.FullName != "Aleksi Lesaj" {
			t.Errorf("annuaire = %+v", ligne)
		}
	}
}

// Nommer ne renomme aucun dépôt : le slug du nouveau nom s'ajoute à ceux que
// la personne portait, et ce qui existe reste à elle.
func TestNommerNeRenommeAucunDepot(t *testing.T) {
	h := college(t)
	h.sansNoms("h27", "5n6", "02", "aleksilesaj")
	avant := h.depots()

	h.json(http.MethodPut, "/api/users/aleksilesaj/name",
		map[string]any{"full_name": "Aleksi Lesaj"}, nil)

	if apres := h.depots(); strings.Join(apres, ",") != strings.Join(avant, ",") {
		t.Fatalf("dépôts :\navant %v\naprès %v", avant, apres)
	}
	// Et le dépôt qu'il portait déjà reste le sien.
	if fiche := h.fiche("aleksilesaj"); fiche.User.Repos != avant0(fiche) {
		t.Errorf("dépôts de la personne = %d", fiche.User.Repos)
	}
}

// Un nom déjà donné se corrige depuis la fiche comme on en donne un à qui n'en
// a pas : ses dépôts restent les siens, et aucun n'est renommé.
func TestCorrigerUnNomDepuisLaFiche(t *testing.T) {
	h := college(t)
	avant := h.depots()

	var rendu struct {
		FullName string `json:"full_name"`
	}
	h.json(http.MethodPut, "/api/users/emilie-cote/name",
		map[string]any{"full_name": "Émilie Côté-Roy"}, &rendu)
	if rendu.FullName != "Émilie Côté-Roy" {
		t.Fatalf("réponse = %+v", rendu)
	}

	fiche := h.fiche("emilie-cote")
	if fiche.User.FullName != "Émilie Côté-Roy" {
		t.Fatalf("nom = %q", fiche.User.FullName)
	}
	if fiche.User.Repos != 3 || avant0(fiche) != 3 {
		t.Errorf("dépôts de la personne = %d, chronologie = %d",
			fiche.User.Repos, avant0(fiche))
	}
	if apres := h.depots(); strings.Join(apres, ",") != strings.Join(avant, ",") {
		t.Fatalf("dépôts :\navant %v\naprès %v", avant, apres)
	}
}

// Une liste de ce poste qui nommait la personne autrement l'emporterait sur le
// registre : le nom corrigé ne se verrait nulle part. Elle le lui cède, et le
// dépôt nommé d'après l'ancien reste le sien.
func TestUneListeLocaleNeMasquePasLeNomCorrige(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", "h27.5n6.02.tp1.aleksi-lesa", true)
	h := avantLeRegistre(t, state, cohorte("h27", "5n6", "02", "Aleksi Lesa", "aleksilesaj"))

	h.json(http.MethodPut, "/api/users/aleksilesaj/name",
		map[string]any{"full_name": "Aleksi Lesaj"}, nil)

	fiche := h.fiche("aleksilesaj")
	if fiche.User.FullName != "Aleksi Lesaj" {
		t.Fatalf("nom = %q : la liste locale masque la correction", fiche.User.FullName)
	}
	if fiche.User.Repos != 1 {
		t.Errorf("dépôts de la personne = %d : l'ancien slug l'a perdue", fiche.User.Repos)
	}
	if ligne, present := ligneDe(h.annuaire(""), "aleksilesaj"); !present ||
		ligne.FullName != "Aleksi Lesaj" {
		t.Errorf("annuaire = %+v", ligne)
	}
	contenu, err := os.ReadFile(h.Groupes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contenu), "Aleksi Lesa\"") {
		t.Errorf("l'ancien nom est resté dans la liste locale :\n%s", contenu)
	}
}

// avant0 rend le nombre de dépôts que la chronologie montre : il doit coller à
// celui de la fiche.
func avant0(fiche ficheRendu) int {
	total := 0
	for _, etape := range fiche.User.Timeline {
		total += len(etape.Assignments)
	}
	return total
}

// Un nom vide ne nomme personne : le refus est dit, pas avalé.
func TestUnNomVideEstRefuse(t *testing.T) {
	h := college(t)
	h.sansNoms("h27", "5n6", "02", "aleksilesaj")

	reponse, contenu := h.requete(http.MethodPut, "/api/users/aleksilesaj/name",
		map[string]any{"full_name": "   "})
	if reponse.StatusCode == http.StatusOK {
		t.Fatalf("un nom vide devait être refusé — %s", contenu)
	}
}

// ------------------------------------------------------ filtre par rôle

// ligneDe retrouve une personne dans l'annuaire.
func ligneDe(rendu annuaireRendu, compte string) (annuaireLigne, bool) {
	for _, ligne := range rendu.Users {
		if ligne.Username == compte {
			return ligne, true
		}
	}
	return annuaireLigne{}, false
}

// Un enseignant figure dans l'annuaire au même titre qu'un étudiant, et le
// filtre sait ne garder que les uns ou que les autres.
func TestLAnnuaireSeFiltreParRole(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)

	tous := h.annuaire("")
	prof, present := ligneDe(tous, "prof")
	if !present {
		t.Fatalf("l'enseignant est absent de l'annuaire : %s", comptesDe(tous))
	}
	if !prof.IsTeacher || prof.Role != "enseignant" {
		t.Errorf("rôle = %+v", prof)
	}

	// Le filtre ne garde que les enseignants…
	profs := h.annuaire("?role=enseignant")
	if comptesDe(profs) != "prof" {
		t.Fatalf("enseignants = %s", comptesDe(profs))
	}
	// … ou que les étudiants, et l'enseignant n'y est plus.
	etudiants := h.annuaire("?role=etudiant")
	if strings.Contains(comptesDe(etudiants), "prof") {
		t.Fatalf("étudiants = %s", comptesDe(etudiants))
	}
	if etudiants.Shown+profs.Shown != tous.Total {
		t.Errorf("%d + %d ≠ %d", etudiants.Shown, profs.Shown, tous.Total)
	}
}

// Un cours donné se distingue d'un cours suivi jusque dans l'annuaire : la
// même colonne porte les deux.
func TestLAnnuaireDitCeQuOnAEteDansChaqueCours(t *testing.T) {
	h := college(t)
	h.coopter("prof", true)
	h.travail(http.MethodPost, "/api/classrooms/a26.5n6.01/teachers",
		map[string]any{"teachers": []string{"prof"}})

	for _, ligne := range h.annuaire("?role=enseignant").Users {
		if ligne.Username != "prof" {
			continue
		}
		if len(ligne.Enrollments) != 1 {
			t.Fatalf("inscriptions = %+v", ligne.Enrollments)
		}
		if ligne.Enrollments[0].Role != "enseignant" ||
			ligne.Enrollments[0].Scope != "a26.5n6.01" {
			t.Errorf("inscription = %+v", ligne.Enrollments[0])
		}
		return
	}
	t.Fatal("l'enseignant est absent")
}

// Un rôle inconnu est refusé plutôt que silencieusement ignoré.
func TestUnRoleInconnuEstRefuse(t *testing.T) {
	h := college(t)
	reponse, contenu := h.requete(http.MethodGet, "/api/users?role=popularite", nil)
	if reponse.StatusCode == http.StatusOK {
		t.Fatalf("un rôle inconnu devait être refusé — %s", contenu)
	}
}
