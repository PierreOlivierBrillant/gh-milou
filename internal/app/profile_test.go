package app_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/users"
)

// La fiche déroule le passage de quelqu'un, de la session la plus récente à la
// plus ancienne : la même chose qu'au navigateur, écrite au terminal.
func TestLaFicheDUnUtilisateurAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Émilie Côté — @emilie-cote", "étudiant",
		"Son passage dans l'organisation",
		// Les trois cours, chacun avec ce qu'il en reste.
		"h27.5n6.02", "a26.4w6.01", "a26.5n6.01",
		"Hiver 2027", "Automne 2026", "projet", "tp1")
}

// Un compte que rien ne connaît rend quand même une fiche : il existe sur
// GitHub, il n'a simplement rien fait ici.
func TestLaFicheDUnCompteInconnuAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "personne-du-tout"

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("@personne-du-tout",
		"ne connaît pas @personne-du-tout",
		"Aucun cours suivi ni donné")
}

// Coopter écrit le rôle au registre. Le premier enseignant se déclare :
// personne ne peut le reconnaître avant lui.
func TestCoopterUnEnseignantAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"
	h.Options.TeacherSet, h.Options.Teacher = true, true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("@emilie-cote est désormais enseignant")

	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if contenu == "" {
		t.Fatal("le registre n'a rien reçu")
	}
	set, soucis := registry.Decode([]byte(contenu))
	if len(soucis) > 0 {
		t.Fatalf("registre = %v", soucis)
	}
	if !set.Teaches("emilie-cote") {
		t.Fatalf("le rôle n'a pas été écrit : %+v", set.All())
	}
}

// Un compte qui n'enseigne pas ne coopte personne. Ce refus ne protège pas le
// registre — GitHub s'en charge — mais il le dit dans les mots de l'outil.
func TestUnNonEnseignantNeCoopteAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("première cooptation : code = %d\n%s", code, h.texte())
	}

	// « prof » n'est toujours pas enseignant, et l'organisation en a un.
	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.User = "jlpicard"
	suivant.Options.TeacherSet, suivant.Options.Teacher = true, true
	if code := suivant.muet(); code == app.ExitOK {
		t.Fatalf("la cooptation devait être refusée\n%s", suivant.texte())
	}
	suivant.contient("Seul un enseignant")
}

// ------------------------------------------------------------ cloisonnement

// Cloisonner un groupe crée son équipe enseignante et lui donne ses dépôts —
// les siens seulement.
func TestCloisonnerUnGroupeAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.User = ""
	suivant.Options.TeacherSet = false
	suivant.Options.ManageRequested, suivant.Options.Manage = true, "a26.5n6.01"
	suivant.Options.TeachersOn = true
	suivant.Options.Teachers = []string{"prof"}
	suivant.Options.Yes = true

	if code := suivant.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, suivant.texte())
	}
	suivant.contient("a26.5n6.01.enseignants", "est cloisonné derrière")

	partages := suivant.State.TeamRepos["acme/a26-5n6-01-enseignants"]
	if len(partages) != 2 {
		t.Fatalf("dépôts partagés = %v", partages)
	}
	for nom, droit := range partages {
		if droit != "admin" {
			t.Errorf("%s : droit = %q", nom, droit)
		}
	}
	// Le groupe voisin n'appartient pas à cette équipe : c'est tout l'objet.
	if _, partage := partages["acme/h27.5n6.02.tp1.emilie-cote"]; partage {
		t.Error("un dépôt d'un autre groupe est allé à cette équipe")
	}
}

// On n'inscrit à l'équipe d'un groupe que quelqu'un que le registre déclare
// enseignant : « is_teacher » ne donne rien, il autorise à donner.
func TestOnNeCloisonnePasDerriereUnEtudiantAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.User = ""
	suivant.Options.TeacherSet = false
	suivant.Options.ManageRequested, suivant.Options.Manage = true, "a26.5n6.01"
	suivant.Options.TeachersOn = true
	suivant.Options.Teachers = []string{"prof", "emilie-cote"}
	suivant.Options.Yes = true

	if code := suivant.muet(); code == app.ExitOK {
		t.Fatalf("inscrire un étudiant devait être refusé\n%s", suivant.texte())
	}
	suivant.contient("emilie-cote")
}

// Un cours donné remonte dans la fiche de celui qui l'a donné : c'est ce qui
// permet de chercher ce qu'un collègue a déjà enseigné.
func TestUnCoursDonneRemonteDansLaFicheAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	cloisonne := nouveauDansLeMemeDossier(t, h)
	cloisonne.Options.User = ""
	cloisonne.Options.TeacherSet = false
	cloisonne.Options.ManageRequested, cloisonne.Options.Manage = true, "a26.5n6.01"
	cloisonne.Options.TeachersOn = true
	cloisonne.Options.Teachers = []string{"prof"}
	cloisonne.Options.Yes = true
	if code := cloisonne.muet(); code != app.ExitOK {
		t.Fatalf("cloisonnement : code = %d\n%s", code, cloisonne.texte())
	}

	fiche := nouveauDansLeMemeDossier(t, h)
	fiche.Options.ManageRequested, fiche.Options.Manage = false, ""
	fiche.Options.TeachersOn = false
	fiche.Options.User = "prof"
	if code := fiche.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, fiche.texte())
	}
	fiche.contient("enseignant", "a26.5n6.01", "a donné ce cours")
}

// Nommer quelqu'un qui n'a pas de nom complet : le nom monte au registre et
// vaut pour tous ses cours, sans qu'aucun dépôt ne soit renommé.
func TestNommerUnUtilisateurAuTerminal(t *testing.T) {
	h := college(t)
	// Un compte repris de dépôts hérités : personne ne l'a jamais nommé.
	h.State.AddRepo("acme", "h27.5n6.02.tp1.aleksilesaj", true)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "5n6", Group: "02",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Aminata Diallo", Username: "aminata-d"},
			{Username: "aleksilesaj"},
		},
	})
	avant := h.State.RepoNames("acme")

	h.Options.StudentsRequested = false
	h.Options.User = "aleksilesaj"
	h.Options.FullName = "Aleksi Lesaj"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("@aleksilesaj s'appelle « Aleksi Lesaj »", "Aleksi Lesaj — @aleksilesaj")

	// Le nom est au registre, et vaut donc pour tout le monde.
	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	if set.Name("aleksilesaj") != "Aleksi Lesaj" {
		t.Fatalf("registre = %+v", set.All())
	}
	// Et le slug de son nouveau nom s'y ajoute sans détacher ses dépôts.
	if trouve, connu := set.Lookup("aleksilesaj"); !connu || trouve.FullName != "Aleksi Lesaj" {
		t.Errorf("son compte doit toujours le désigner : %+v, %v", trouve, connu)
	}
	if trouve, connu := set.Lookup("aleksi-lesaj"); !connu ||
		trouve.Username != "aleksilesaj" {
		t.Errorf("le slug du nom doit le désigner aussi : %+v, %v", trouve, connu)
	}
	// Aucun dépôt d'étudiant n'a bougé. Le dépôt de service « .cohorte », lui,
	// vient de naître : c'est là que le nom a été écrit.
	apres := sansService(h.State.RepoNames("acme"))
	if strings.Join(apres, ",") != strings.Join(sansService(avant), ",") {
		t.Fatalf("dépôts :\navant %v\naprès %v", avant, apres)
	}
}

// Corriger un nom qu'une liste de ce poste donnait autrement : la liste
// l'emporterait sur le registre, si bien qu'elle le lui cède. Le dépôt nommé
// d'après l'ancien nom reste le sien.
func TestCorrigerUnNomAuTerminal(t *testing.T) {
	h := college(t)
	h.State.AddRepo("acme", "h27.5n6.02.tp1.aleksi-lesa", true)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "5n6", Group: "02",
		Students: []roster.Person{{FullName: "Aleksi Lesa", Username: "aleksilesaj"}},
	})

	h.Options.StudentsRequested = false
	h.Options.User = "aleksilesaj"
	h.Options.FullName = "Aleksi Lesaj"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Aleksi Lesaj — @aleksilesaj", "tp1")
	h.absent("Aleksi Lesa —")

	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	if trouve, connu := set.Lookup("aleksi-lesa"); !connu || trouve.Username != "aleksilesaj" {
		t.Errorf("l'ancien slug doit toujours le désigner : %+v, %v", trouve, connu)
	}
}

// Au terminal, la fiche propose de corriger un nom déjà donné, et dit que ses
// dépôts ne sont pas renommés pour autant.
func TestCorrigerUnNomDepuisLaFicheInteractive(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"

	// Corriger ? oui ; le nom ; coopter ? non ; ses comptes : rien.
	code, scripte := h.script("o", "Émilie Côté-Roy", "n", "rien")
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	question, posee := scripte.AskedFor("Nom complet")
	if !posee || question.Default != "Émilie Côté" {
		t.Fatalf("le champ doit être prérempli du nom courant : %+v, %v", question, posee)
	}
	h.contient("Aucun dépôt n'est renommé",
		"@emilie-cote s'appelle « Émilie Côté-Roy »")

	contenu := h.State.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	set, _ := registry.Decode([]byte(contenu))
	if set.Name("emilie-cote") != "Émilie Côté-Roy" {
		t.Fatalf("registre = %+v", set.All())
	}
}

// Un nom laissé tel quel n'écrit rien : l'annoncer comme une correction ferait
// croire à un geste qui n'a pas eu lieu.
func TestUnNomInchangeNEcritRienAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"

	// Corriger ? oui ; Entrée garde le nom proposé ; coopter ? non ; ses
	// comptes : rien.
	if code, _ := h.script("o", "", "n", "rien"); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Le nom n'a pas changé.")
	h.absent("s'appelle")
}

// Un nom vide ne nomme personne : le refus est dit plutôt qu'avalé.
func TestUnNomVideEstRefuseAuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "emilie-cote"
	h.Options.FullName = "   "

	// Un nom fait d'espaces n'est pas une demande : la fiche s'affiche, sans
	// que rien ne soit écrit.
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.absent("s'appelle")
}

// sansService écarte les dépôts de service — « .cohorte » et les autres — pour
// ne comparer que ce qui appartient aux étudiants.
func sansService(noms []string) []string {
	ordinaires := make([]string, 0, len(noms))
	for _, nom := range noms {
		if !strings.HasPrefix(nom, ".") {
			ordinaires = append(ordinaires, nom)
		}
	}
	return ordinaires
}

// ---------------------------------------------------------- filtre par rôle

// L'annuaire du terminal montre le rôle et se filtre dessus, comme celui du
// navigateur : c'est le même paquet qui décide de ce que « enseignant » veut
// dire.
func TestLAnnuaireDuTerminalSeFiltreParRole(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	// Sans filtre : tout le monde, et l'enseignant est de la partie bien qu'il
	// ne figure sur aucune liste de classe.
	tous := nouveauDansLeMemeDossier(t, h)
	tous.Options.User = ""
	tous.Options.TeacherSet = false
	tous.Options.StudentsRequested = true
	if code := tous.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, tous.texte())
	}
	tous.contient("@prof", "enseignant", "Émilie Côté")

	// Avec le filtre : les enseignants seulement.
	profs := nouveauDansLeMemeDossier(t, h)
	profs.Options.User = ""
	profs.Options.TeacherSet = false
	profs.Options.StudentsRequested = true
	profs.Options.Filter.Role = users.OnlyTeachers
	if code := profs.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, profs.texte())
	}
	profs.contient("@prof", "enseignants seulement")
	profs.absent("Émilie Côté", "Jean-Luc Picard")
}

// Un cours donné se marque d'une étoile, et l'étoile s'explique : une marque
// que rien ne nomme ne dit rien.
func TestUnCoursDonneSeMarqueDansLAnnuaireDuTerminal(t *testing.T) {
	h := college(t)
	h.Options.StudentsRequested = false
	h.Options.User = "prof"
	h.Options.TeacherSet, h.Options.Teacher = true, true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("cooptation : code = %d\n%s", code, h.texte())
	}

	cloisonne := nouveauDansLeMemeDossier(t, h)
	cloisonne.Options.User = ""
	cloisonne.Options.TeacherSet = false
	cloisonne.Options.ManageRequested, cloisonne.Options.Manage = true, "a26.5n6.01"
	cloisonne.Options.TeachersOn = true
	cloisonne.Options.Teachers = []string{"prof"}
	cloisonne.Options.Yes = true
	if code := cloisonne.muet(); code != app.ExitOK {
		t.Fatalf("cloisonnement : code = %d\n%s", code, cloisonne.texte())
	}

	liste := nouveauDansLeMemeDossier(t, h)
	liste.Options.User = ""
	liste.Options.TeacherSet = false
	liste.Options.ManageRequested, liste.Options.Manage = false, ""
	liste.Options.TeachersOn = false
	liste.Options.StudentsRequested = true
	if code := liste.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, liste.texte())
	}
	liste.contient("a26.5n6.01*", "Un « * » marque un cours donné plutôt que suivi.")
}
