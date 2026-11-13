package classroom_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/config"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

// depots construit un inventaire à partir de noms.
func depots(noms ...string) []groups.RepoInfo {
	inventaire := make([]groups.RepoInfo, 0, len(noms))
	for _, nom := range noms {
		inventaire = append(inventaire, groups.RepoInfo{Name: nom, Private: true})
	}
	return inventaire
}

// personnes construit une liste d'étudiants « nom complet / compte ».
func personnes(couples ...string) []roster.Person {
	liste := make([]roster.Person, 0, len(couples)/2)
	for index := 0; index+1 < len(couples); index += 2 {
		liste = append(liste, roster.Person{FullName: couples[index], Username: couples[index+1]})
	}
	return liste
}

// groupe déclare un groupe de la nomenclature courante.
func groupe(session, cours, section string, etudiants []roster.Person) classroom.Classroom {
	return classroom.Classroom{
		Org: "acme", Session: session, Course: cours, Group: section,
		Students: etudiants,
		Defaults: classroom.DefaultsFrom(config.Default()),
	}
}

func noms(travaux []classroom.Assignment) []string {
	liste := make([]string, 0, len(travaux))
	for _, travail := range travaux {
		liste = append(liste, travail.Name)
	}
	return liste
}

var cohorte = personnes(
	"Émilie Côté", "emilie-cote",
	"Jean-Luc Picard", "jlpicard",
	"Aminata Diallo", "aminata-d",
)

// ------------------------------------------------------------ identifiants

func TestIdentifiantDeTravail(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)
	if id := cours.AssignmentID("tp1"); id != "a26.5n6.01.tp1" {
		t.Fatalf("identifiant %q", id)
	}
	if court := cours.ShortName("a26.5n6.01.travail-session"); court != "travail-session" {
		t.Fatalf("nom court %q", court)
	}
	if !cours.Owns("a26.5n6.01.tp1") || cours.Owns("a26.5n6.02.tp1") || cours.Owns("a26.5n6.01") {
		t.Fatal("le périmètre du groupe est mal délimité")
	}
	if cours.Scope() != "a26.5n6.01" {
		t.Fatalf("portée %q", cours.Scope())
	}
}

func TestPlusieursGroupesDansUnMemeCours(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.02.tp1.jean-luc-picard",
	)
	premier := groupe("a26", "5n6", "01", cohorte)
	second := groupe("a26", "5n6", "02", cohorte)

	if travaux := premier.Assignments(inventaire, nil); len(travaux) != 1 ||
		travaux[0].Repos != 1 || travaux[0].Students != 1 {
		t.Fatalf("groupe 01 : %+v", travaux)
	}
	if travaux := second.Assignments(inventaire, nil); len(travaux) != 1 ||
		travaux[0].Repos != 1 || travaux[0].Students != 1 {
		t.Fatalf("groupe 02 : %+v", travaux)
	}
}

// ---------------------------------------------------------------- travaux

func TestTravauxDuGroupe(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.travail-session.emilie-cote", "a26.5n6.01.travail-session.jean-luc-picard",
		"a26.5n6.01.tp1.emilie-cote",
		"a26.4w6.01.tp1.jean-luc-picard",
		"notes-du-cours",
	)
	cours := groupe("a26", "5n6", "01", cohorte)

	travaux := cours.Assignments(inventaire, nil)
	if len(travaux) != 2 {
		t.Fatalf("travaux trouvés : %v", noms(travaux))
	}
	trouves := map[string]classroom.Assignment{}
	for _, travail := range travaux {
		trouves[travail.Name] = travail
	}
	if trouves["travail-session"].Repos != 2 || trouves["travail-session"].Students != 2 {
		t.Fatalf("travail-session : %+v", trouves["travail-session"])
	}
	// Un travail distribué à une seule personne se lit aussi bien : le
	// séparateur réservé n'exige pas deux dépôts pour conclure.
	if trouves["tp1"].Repos != 1 || trouves["tp1"].Students != 1 {
		t.Fatalf("tp1 : %+v", trouves["tp1"])
	}
	// Le cours voisin ne déborde pas.
	if _, present := trouves["4w6"]; present {
		t.Fatalf("un dépôt d'un autre cours a été rattaché : %v", noms(travaux))
	}
}

func TestDepotHorsListeCompteApart(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.tp1.jean-luc-picard",
		"a26.5n6.01.tp1.visiteur-inconnu",
	)
	cours := groupe("a26", "5n6", "01", cohorte)

	travaux := cours.Assignments(inventaire, nil)
	if len(travaux) != 1 {
		t.Fatalf("travaux trouvés : %v", noms(travaux))
	}
	if travaux[0].Students != 2 || travaux[0].Others != 1 {
		t.Fatalf("comptage : %+v", travaux[0])
	}
}

// annuaire joue le registre de l'organisation : il répond au dernier niveau
// d'un nom de dépôt. Une fiche peut n'y porter qu'un compte — c'est le cas de
// tout ce qui a été repris de dépôts hérités.
type annuaire map[string]roster.Person

func (a annuaire) Lookup(fragment string) (roster.Person, bool) {
	personne, connue := a[strings.ToLower(fragment)]
	return personne, connue
}

// Un dépôt qui porte le compte de quelqu'un dont on n'a jamais eu le nom
// complet désigne quand même une personne du groupe. Sans cela, elle ne
// figurait dans aucune liste — donc nulle part où la nommer ni la déplacer —
// alors que c'est justement d'elle qu'il manque le nom.
func TestUnCompteSansNomCompletResteUnEtudiantDuGroupe(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.tp1.aleksilesaj",
		"a26.5n6.01.tp1.visiteur-inconnu",
	)
	connus := annuaire{
		"emilie-cote": roster.Person{FullName: "Émilie Côté", Username: "emilie-cote"},
		"aleksilesaj": roster.Person{Username: "aleksilesaj"},
	}
	cours := groupe("a26", "5n6", "01", nil).Enrich(connus, inventaire)

	sansNom, inscrit := cours.Find("aleksilesaj")
	if !inscrit {
		t.Fatalf("étudiants déduits : %v", cours.Students)
	}
	if sansNom.FullName != "" {
		t.Errorf("nom complet = %q : rien ne doit être inventé", sansNom.FullName)
	}
	// Son dépôt lui est rattaché : il compte parmi ceux du groupe, et non
	// parmi les orphelins.
	if _, sien := cours.StudentOf("a26.5n6.01.tp1.aleksilesaj"); !sien {
		t.Error("le dépôt doit être rattaché à son étudiant")
	}
	travaux := cours.Assignments(inventaire, nil)
	if len(travaux) != 1 || travaux[0].Students != 2 || travaux[0].Others != 1 {
		t.Fatalf("comptage : %+v", travaux)
	}
	// Ce que le registre ignore reste hors liste : un slug n'est pas une
	// personne de plus.
	if _, invente := cours.Find("visiteur-inconnu"); invente {
		t.Error("un dépôt que personne ne réclame ne doit inscrire personne")
	}
}

// lesNoms rend les noms complets d'une liste, dans son ordre.
func lesNoms(gens []roster.Person) string {
	noms := make([]string, 0, len(gens))
	for _, personne := range gens {
		noms = append(noms, personne.FullName)
	}
	return strings.Join(noms, ", ")
}

func TestRecipientsEcarteLesDejaServis(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote")
	cours := groupe("a26", "5n6", "01", cohorte)

	attendus, servis := cours.Recipients("a26.5n6.01.tp1", inventaire, nil)
	if lesNoms(servis) != "Émilie Côté" ||
		lesNoms(attendus) != "Jean-Luc Picard, Aminata Diallo" {
		t.Fatalf("attendus : %s ; servis : %s", lesNoms(attendus), lesNoms(servis))
	}
}

// Une cohorte importée du collège n'a que des matricules : c'est par eux que
// la sélection désigne ces personnes, comme Find les désigne. Et le dépôt de
// l'une ne tient pas les autres pour servies : elles n'ont pas un compte en
// commun, elles n'en ont aucun.
func TestRecipientsDesigneParSonMatriculeQuiNAPasDeCompte(t *testing.T) {
	cours := groupe("a26", "5n6", "01", append(personnes("Jean-Luc Picard", "jlpicard"),
		roster.Person{FullName: "Nadia Pépin", StudentID: "2100110"},
		roster.Person{FullName: "Aminata Diallo", StudentID: "2100789"},
	))
	inventaire := depots("a26.5n6.01.tp1.nadia-pepin")

	attendus, servis := cours.Recipients("a26.5n6.01.tp1", inventaire,
		[]string{"2100110", "2100789"})
	if lesNoms(attendus) != "Aminata Diallo" || lesNoms(servis) != "Nadia Pépin" {
		t.Errorf("sélection par matricule — attendus : %s ; servis : %s",
			lesNoms(attendus), lesNoms(servis))
	}

	attendus, _ = cours.Recipients("a26.5n6.01.tp1", inventaire, nil)
	if lesNoms(attendus) != "Jean-Luc Picard, Aminata Diallo" {
		t.Errorf("sans sélection, tout le groupe attend sauf Nadia : %s", lesNoms(attendus))
	}

	// Une désignation vide ne désigne personne : c'est elle que l'aperçu
	// envoyait pour qui n'avait pas de compte, et sélectionner tout le monde
	// revenait alors à l'oublier.
	for _, selection := range [][]string{{}, {""}} {
		attendus, servis = cours.Recipients("a26.5n6.01.tp1", inventaire, selection)
		if len(attendus)+len(servis) != 0 {
			t.Errorf("%q ne retient personne : attendus %s ; servis %s",
				selection, lesNoms(attendus), lesNoms(servis))
		}
	}
}

// « ajouter », au terminal, ne connaît un travail que par ses dépôts : il doit
// y reconnaître qui est déjà servi comme le navigateur le reconnaît. Le dernier
// niveau d'un nom de dépôt désigne une personne par son nom — c'est ce que la
// nomenclature écrit — ou par l'un de ses comptes, comme les dépôts hérités de
// GitHub Classroom. Le comparer au seul compte laissait pour neuve une personne
// servie sous la nomenclature : « jean-luc-picard » n'est pas « jlpicard ».
func TestPendingReconnaitQuiADejaSonDepot(t *testing.T) {
	for _, cas := range []struct {
		nom, prefixe, depot, visiteur string
	}{
		{"nomenclature, par le nom", "a26.5n6.01.tp1",
			"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.visiteur"},
		{"préfixe hérité, par le compte", "tp1", "tp1-jlpicard", "tp1-visiteur"},
		{"préfixe hérité, par le nom", "tp1", "tp1-jean-luc-picard", "tp1-visiteur"},
		{"marque de doublon de GitHub", "tp1", "tp1-JLPicard-1", "tp1-visiteur"},
	} {
		t.Run(cas.nom, func(t *testing.T) {
			// Le dépôt d'un visiteur que la liste ignore ne sert personne.
			travail := groups.Build(cas.prefixe, depots(cas.depot, cas.visiteur))
			attendus, servis := classroom.Pending(cohorte, travail, nil)
			if lesNoms(servis) != "Jean-Luc Picard" ||
				lesNoms(attendus) != "Émilie Côté, Aminata Diallo" {
				t.Errorf("attendus : %s ; servis : %s", lesNoms(attendus), lesNoms(servis))
			}
		})
	}
}

// Une inscrite sans compte se reconnaît à son nom, et son dépôt ne tient pas les
// autres pour servies : elles ne partagent pas un compte, elles n'en ont aucun.
// Le registre rattache en plus un dépôt que le nom courant ne produit plus — un
// nom corrigé depuis —, comme il le fait au navigateur : sans lui, Jean-Luc en
// recevrait un second sous son nom corrigé.
func TestPendingSansCompteEtSousUnNomCorrige(t *testing.T) {
	liste := append(personnes("Jean-Luc Picard", "jlpicard"),
		roster.Person{FullName: "Nadia Pépin", StudentID: "2100110"},
		roster.Person{FullName: "Aminata Diallo", StudentID: "2100789"},
	)
	travail := groups.Build("a26.5n6.01.tp1",
		depots("a26.5n6.01.tp1.nadia-pepin", "a26.5n6.01.tp1.jean-luc-picar"))
	connus := annuaire{
		"jean-luc-picar": {FullName: "Jean-Luc Picar", Username: "jlpicard"},
	}

	attendus, servis := classroom.Pending(liste, travail, connus)
	if lesNoms(servis) != "Jean-Luc Picard, Nadia Pépin" || lesNoms(attendus) != "Aminata Diallo" {
		t.Errorf("avec le registre — attendus : %s ; servis : %s",
			lesNoms(attendus), lesNoms(servis))
	}
	attendus, _ = classroom.Pending(liste, travail, nil)
	if lesNoms(attendus) != "Jean-Luc Picard, Aminata Diallo" {
		t.Errorf("sans registre, le nom d'avant n'est à personne : attendus %s",
			lesNoms(attendus))
	}
}

func TestDepotRattacheASonEtudiant(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)

	student, inscrit := cours.StudentOf("a26.5n6.01.tp1.jean-luc-picard")
	if !inscrit || student.Username != "jlpicard" {
		t.Fatalf("étudiant retrouvé : %+v (%v)", student, inscrit)
	}
	if _, inscrit := cours.StudentOf("a26.5n6.01.tp1.inconnu"); inscrit {
		t.Fatal("un dépôt hors liste a été rattaché")
	}
}

func TestNomCompletManquantEmpecheDeNommer(t *testing.T) {
	cours := groupe("a26", "5n6", "01", append(
		personnes("Émilie Côté", "emilie-cote"),
		roster.Person{Username: "sans-nom"},
	))
	incomplets := cours.MissingNames()
	if len(incomplets) != 1 || incomplets[0].Username != "sans-nom" {
		t.Fatalf("étudiants incomplets : %+v", incomplets)
	}
}

func TestReglagesDuTravailReprennentLeGroupe(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)
	cours.Defaults.Visibility = "public"
	cours.Defaults.Template = "acme/modele"

	reglages := cours.Settings("tp1")
	if reglages.Assignment != "a26.5n6.01.tp1" {
		t.Fatalf("travail %q", reglages.Assignment)
	}
	if reglages.NamePattern != classroom.NamePattern {
		t.Fatalf("gabarit %q : il n'est pas réglable", reglages.NamePattern)
	}
	if reglages.Org != "acme" || reglages.Visibility != "public" ||
		reglages.Template != "acme/modele" {
		t.Fatalf("réglages : %+v", reglages)
	}
}

// ------------------------------------------------------- ancienne nomenclature

// --------------------------------------------------------------- candidats

// ----------------------------------------------------------------- magasin

func TestMagasinEcritEtRelit(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)

	cree, err := magasin.Save(groupe("a26", "5n6", "01", cohorte))
	if err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	// Un groupe se désigne par sa place, pas par un numéro inventé ici.
	if cree.Scope() != "a26.5n6.01" {
		t.Fatalf("place %q", cree.Scope())
	}

	if _, err := os.Stat(chemin); err != nil {
		t.Fatalf("fichier absent : %v", err)
	}
	// Le fichier contient des noms d'étudiants : il est écrit en 0600. Windows
	// ne porte pas ces bits — les y vérifier ne dirait rien.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(chemin)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("permissions %o, attendu 600", mode)
		}
	}

	relu := classroom.Open(chemin)
	retrouve, present := relu.Find("acme", "a26.5n6.01")
	if !present || retrouve.Session != "a26" || retrouve.Course != "5n6" ||
		retrouve.Group != "01" || len(retrouve.Students) != 3 {
		t.Fatalf("groupe relu : %+v", retrouve)
	}
	// La casse ne distingue pas deux dépôts : elle ne distingue pas deux places.
	if _, present := relu.Find("ACME", "A26.5N6.01"); !present {
		t.Fatal("la place devrait se retrouver sans égard à la casse")
	}

	retrouve.Defaults.Template = "acme/modele"
	if _, err := relu.Save(retrouve); err != nil {
		t.Fatalf("mise à jour : %v", err)
	}
	encore := classroom.Open(chemin).List("acme")
	if len(encore) != 1 || encore[0].Defaults.Template != "acme/modele" {
		t.Fatalf("mise à jour non enregistrée : %+v", encore)
	}

	if err := relu.Forget("acme", "a26.5n6.01"); err != nil {
		t.Fatalf("oubli : %v", err)
	}
	if reste := classroom.Open(chemin).List("acme"); len(reste) != 0 {
		t.Fatalf("groupe encore retenu : %+v", reste)
	}
}

func TestMagasinSuitUnGroupeQuiChangeDePlace(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)
	if _, err := magasin.Save(groupe("a26", "5n6", "01", cohorte)); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	// Les dépôts viennent d'être renommés : la liste doit les suivre.
	if _, err := magasin.Move("acme", "a26.5n6.01", groupe("h27", "5n6", "02", cohorte)); err != nil {
		t.Fatalf("déplacement : %v", err)
	}
	relu := classroom.Open(chemin)
	if _, present := relu.Find("acme", "a26.5n6.01"); present {
		t.Fatal("l'ancienne place ne devrait plus rien retenir")
	}
	arrivee, present := relu.Find("acme", "h27.5n6.02")
	if !present || len(arrivee.Students) != 3 {
		t.Fatalf("nouvelle place : %+v", arrivee)
	}
}

// Un nom corrigé au registre ne doit pas rester masqué par celui qu'une liste
// de ce poste donnait : il est retiré de toutes les listes de l'organisation,
// et d'elles seules.
func TestMagasinCedeUnNomAuRegistre(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)
	for _, cours := range []classroom.Classroom{
		groupe("a26", "5n6", "01", personnes("Emlie Côté", "emilie-cote", "Jean-Luc Picard", "jlpicard")),
		groupe("h27", "5n6", "02", personnes("Emilie Coté", "Emilie-Cote")),
	} {
		if _, err := magasin.Save(cours); err != nil {
			t.Fatalf("enregistrement : %v", err)
		}
	}
	ailleurs := groupe("a26", "5n6", "01", personnes("Emlie Côté", "emilie-cote"))
	ailleurs.Org = "autre"
	if _, err := magasin.Save(ailleurs); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	retires, err := magasin.Unname("acme", "EMILIE-COTE")
	if err != nil || retires != 2 {
		t.Fatalf("retirés = %d, %v", retires, err)
	}

	relu := classroom.Open(chemin)
	for _, personne := range relu.People("acme") {
		voulu := ""
		if personne.Username == "jlpicard" {
			voulu = "Jean-Luc Picard"
		}
		if personne.FullName != voulu {
			t.Errorf("@%s : nom = %q, attendu %q", personne.Username, personne.FullName, voulu)
		}
	}
	// Une autre organisation a son propre registre : rien n'y est cédé.
	if gens := relu.People("autre"); len(gens) != 1 || gens[0].FullName != "Emlie Côté" {
		t.Errorf("autre organisation : %+v", gens)
	}

	// Un compte qu'aucune liste ne nomme n'écrit rien.
	if retires, err := relu.Unname("acme", "emilie-cote"); err != nil || retires != 0 {
		t.Errorf("second retrait : %d, %v", retires, err)
	}
}

func TestMagasinNePartagePasSesTranches(t *testing.T) {
	magasin := classroom.Open(filepath.Join(t.TempDir(), "groupes.json"))
	if _, err := magasin.Save(groupe("a26", "5n6", "01", personnes("", "emilie-cote"))); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	// Modifier ce qu'on a reçu ne doit pas écrire dans le magasin.
	rendu, _ := magasin.Find("acme", "a26.5n6.01")
	rendu.Students[0].FullName = "Écrit par mégarde"

	encore, _ := magasin.Find("acme", "a26.5n6.01")
	if encore.Students[0].FullName != "" {
		t.Fatalf("le magasin a été modifié dans son dos : %q", encore.Students[0].FullName)
	}
}

func TestMagasinNeGardeQuUneListeParPlace(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)
	if _, err := magasin.Save(groupe("a26", "5n6", "01", cohorte)); err != nil {
		t.Fatalf("premier enregistrement : %v", err)
	}
	// Réenregistrer la même place remplace, sans créer de doublon : deux
	// listes pour les mêmes dépôts n'auraient aucun sens.
	if _, err := magasin.Save(groupe("a26", "5n6", "01", personnes("Aminata Diallo", "aminata-d"))); err != nil {
		t.Fatalf("second enregistrement : %v", err)
	}
	retenus := magasin.List("acme")
	if len(retenus) != 1 || len(retenus[0].Students) != 1 {
		t.Fatalf("groupes retenus : %+v", retenus)
	}
	// Un autre groupe du même cours reste un autre groupe.
	if _, err := magasin.Save(groupe("a26", "5n6", "02", cohorte)); err != nil {
		t.Fatalf("second groupe du cours : %v", err)
	}
	if len(magasin.List("acme")) != 2 {
		t.Fatalf("groupes retenus : %+v", magasin.List("acme"))
	}
}

func TestPlaceOuvreUnGroupeJamaisDeclare(t *testing.T) {
	// Un groupe existe parce que ses dépôts existent : il s'ouvre sans avoir
	// été déclaré nulle part.
	cours, err := classroom.AtScope("acme", "a26.5n6.01", classroom.DefaultsFrom(config.Default()))
	if err != nil {
		t.Fatalf("place : %v", err)
	}
	if cours.Session != "a26" || cours.Course != "5n6" || cours.Group != "01" {
		t.Fatalf("groupe composé : %+v", cours)
	}
	if cours.Label() != "Groupe 01" || cours.SessionName() != "Automne 2026" {
		t.Fatalf("libellés : %q / %q", cours.Label(), cours.SessionName())
	}

	// Ce qui ne porte pas trois niveaux n'est pas une place, et le dire vaut
	// mieux que d'ouvrir un groupe qui ne trouverait aucun dépôt.
	for _, hors := range []string{"a26-5n6", "a26.5n6", "projet-{assignment}-{student}"} {
		if _, err := classroom.AtScope("acme", hors,
			classroom.DefaultsFrom(config.Default())); err == nil {
			t.Errorf("« %s » a été acceptée comme place", hors)
		}
	}
}

func TestPlacesLuesDansLesDepots(t *testing.T) {
	places := classroom.Places(depots(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp2.emilie-cote",
		"a26.4w6.02.tp1.jlpicard",
		"a26-5n6-tp1-jlpicard", // nomenclature dépassée : rien à en tirer
	))
	if strings.Join(places, ",") != "a26.4w6.02,a26.5n6.01" {
		t.Fatalf("places : %v", places)
	}
}

func TestMagasinRefuseUnSeparateurDansUnNiveau(t *testing.T) {
	magasin := classroom.Open(filepath.Join(t.TempDir(), "groupes.json"))

	// Le point saisi dans un champ est slugifié, jamais conservé : le groupe
	// « 01.b » devient « 01-b » et la nomenclature garde ses cinq niveaux.
	cree, err := magasin.Save(groupe("a26", "5n6", "01.b", cohorte))
	if err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	if cree.Group != "01-b" {
		t.Fatalf("groupe enregistré : %q", cree.Group)
	}
	if strings.Count(cree.Scope(), ".") != 2 {
		t.Fatalf("portée mal découpée : %q", cree.Scope())
	}
}

func TestSessionsDesGroupesRetenus(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)
	for _, cours := range []classroom.Classroom{
		groupe("a26", "5n6", "01", cohorte),
		groupe("a26", "4w6", "01", cohorte),
		groupe("h27", "5n6", "01", cohorte),
	} {
		if _, err := magasin.Save(cours); err != nil {
			t.Fatalf("enregistrement : %v", err)
		}
	}

	// Le nom long ne s'écrit nulle part : il se déduit du nom court. La plus
	// récente des sessions vient la première.
	sessions := classroom.Open(chemin).Sessions("acme")
	if len(sessions) != 2 || sessions[0].Short != "h27" ||
		sessions[0].Name != "Hiver 2027" || sessions[1].Name != "Automne 2026" {
		t.Fatalf("sessions : %+v", sessions)
	}
	// Une autre organisation n'a rien à voir ici.
	if autres := classroom.Open(chemin).Sessions("college"); len(autres) != 0 {
		t.Fatalf("sessions d'une autre organisation : %+v", autres)
	}
}

func TestNomDeSessionDeduitDeLaSaison(t *testing.T) {
	cas := map[string]string{
		"a26": "Automne 2026", "h27": "Hiver 2027",
		"e27": "Été 2027", "p28": "Printemps 2028",
		"A26": "Automne 2026",
		// Ce qui ne suit pas la convention se rend tel quel.
		"x26": "x26", "automne": "automne", "a2026": "a2026",
	}
	for court, attendu := range cas {
		if nom := naming.SessionLabel(court); nom != attendu {
			t.Errorf("%s → %q, attendu %q", court, nom, attendu)
		}
	}
}

func TestSessionsRangeesDeLaPlusRecenteALaPlusAncienne(t *testing.T) {
	// L'ordre remonte le calendrier : la session en cours d'abord, et de là
	// vers les plus anciennes — l'automne, l'été, le printemps, l'hiver, année
	// après année.
	sessions := classroom.SessionsOf([]string{
		"a27", "e26", "h27", "p26", "a26", "H27", "h26", "e27", "p27",
	})
	rendu := make([]string, 0, len(sessions))
	for _, session := range sessions {
		rendu = append(rendu, session.Short)
	}
	attendu := "a27 e27 p27 h27 a26 e26 p26 h26"
	if strings.Join(rendu, " ") != attendu {
		t.Fatalf("ordre : %q, attendu %q", strings.Join(rendu, " "), attendu)
	}
}

func TestSessionsHorsConventionPassentApres(t *testing.T) {
	// Rien n'oblige à suivre la convention : ce qui ne s'y lit pas ne se range
	// pas dans le calendrier, mais ne disparaît pas pour autant.
	sessions := classroom.SessionsOf([]string{"zeta", "a27", "  ", "alpha", "h26", "alpha"})
	rendu := make([]string, 0, len(sessions))
	for _, session := range sessions {
		rendu = append(rendu, session.Short)
	}
	attendu := "a27 h26 alpha zeta"
	if strings.Join(rendu, " ") != attendu {
		t.Fatalf("ordre : %q, attendu %q", strings.Join(rendu, " "), attendu)
	}
}

// ------------------------------------------------------- marques de doublon

// GitHub refuse deux dépôts de même nom dans une organisation : quand celui
// qu'on demandait est pris, il ajoute « -1 ». La marque se pose sur le compte
// quand c'est lui qui termine le nom, et l'adoption la lisait comme un autre
// compte — alors que « jlpicard-1 » n'est le compte de personne.
func TestCompteMarqueRejointLeSien(t *testing.T) {
	cours := groupe("a26", "5n6", "01", append(
		append([]roster.Person(nil), cohorte...),
		roster.Person{Username: "jlpicard-1"},
	))
	valide, err := cours.Validate()
	if err != nil {
		t.Fatalf("validation : %v", err)
	}
	if len(valide.Students) != len(cohorte) {
		t.Fatalf("liste : %+v", valide.Students)
	}
	if _, marque := valide.Find("jlpicard-1"); marque {
		t.Error("le compte marqué est resté dans la liste")
	}
}

// Sans le compte de base pour l'attester, la marque ne prouve rien : « CO-7 »
// est un vrai compte, et le lui retirer inventerait quelqu'un.
func TestCompteQuiFinitParUnNombreEstLaisseIntact(t *testing.T) {
	cours := groupe("a26", "5n6", "01", personnes("Claire Otis", "CO-7"))
	valide, err := cours.Validate()
	if err != nil {
		t.Fatalf("validation : %v", err)
	}
	if len(valide.Students) != 1 || valide.Students[0].Username != "CO-7" {
		t.Fatalf("liste : %+v", valide.Students)
	}
}

// Deux noms complets qui se contredisent sont deux personnes : la marque reste,
// et c'est à quelqu'un de trancher.
func TestDeuxNomsDifferentsNeSeFondentPas(t *testing.T) {
	cours := groupe("a26", "5n6", "01", personnes(
		"Jean-Luc Picard", "jlpicard",
		"Jeanne Picard", "jlpicard-1",
	))
	valide, err := cours.Validate()
	if err != nil {
		t.Fatalf("validation : %v", err)
	}
	if len(valide.Students) != 2 {
		t.Fatalf("liste : %+v", valide.Students)
	}
}

// Le dépôt que la marque de doublon a fait dévier reste celui de son étudiant :
// sans cela, corriger la liste le rendrait orphelin.
func TestDepotMarqueResteRattacheASonEtudiant(t *testing.T) {
	cas := []struct {
		nom    string
		cours  classroom.Classroom
		depot  string
		compte string
	}{
		{"par le nom", groupe("a26", "5n6", "01", cohorte),
			"a26.5n6.01.tp1.jean-luc-picard-1", "jlpicard"},
		{"par le compte", groupe("a26", "5n6", "01",
			classroom.StudentsOf([]string{"jlpicard"})),
			"a26.5n6.01.tp1.jlpicard-1", "jlpicard"},
	}
	for _, essai := range cas {
		t.Run(essai.nom, func(t *testing.T) {
			student, inscrit := essai.cours.StudentOf(essai.depot)
			if !inscrit || student.Username != essai.compte {
				t.Fatalf("étudiant retrouvé : %+v (%v)", student, inscrit)
			}
		})
	}
}

// La collision qui a produit la marque est à l'échelle de l'organisation : le
// compte qui l'atteste est souvent dans un autre groupe — une session
// précédente, un autre cours.
func TestMagasinCorrigeUnCompteMarqueDUnAutreGroupe(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "groupes.json")
	magasin := classroom.Open(chemin)
	if _, err := magasin.Save(groupe("h24", "4n6", "1020",
		personnes("Alexis Lesage", "aleksilesaj"))); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}
	if _, err := magasin.Save(groupe("a24", "5n6", "1030",
		personnes("Alexis Lesage", "aleksilesaj-1"))); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	relu, present := classroom.Open(chemin).Find("acme", "a24.5n6.1030")
	if !present {
		t.Fatal("groupe introuvable")
	}
	if len(relu.Students) != 1 || relu.Students[0].Username != "aleksilesaj" {
		t.Fatalf("liste relue : %+v", relu.Students)
	}
}

// Publier le registre part de tout ce qu'un poste sait des personnes : les
// doublons y restent, car un compte nommé de deux façons est ce qu'il faut
// montrer avant de trancher.
func TestPersonnesDeTousLesGroupes(t *testing.T) {
	dossier := t.TempDir()
	store := classroom.Open(filepath.Join(dossier, "groupes.json"))
	if _, err := store.Save(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "5n6", Group: "02",
		Students: []roster.Person{{FullName: "Emlie Côté", Username: "ecote"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "ecote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(classroom.Classroom{
		Org: "autre", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Ailleurs", Username: "ailleurs"}},
	}); err != nil {
		t.Fatal(err)
	}

	gens := store.People("acme")
	// Rangés par place : « a26… » avant « h27… », quel que soit l'ordre d'écriture.
	if len(gens) != 3 || gens[0].Username != "ecote" || gens[0].FullName != "Émilie Côté" {
		t.Fatalf("personnes = %+v", gens)
	}
	if gens[2].FullName != "Emlie Côté" {
		t.Fatalf("le doublon n'a pas été conservé : %+v", gens)
	}
	for _, person := range gens {
		if person.Username == "ailleurs" {
			t.Error("une autre organisation s'est glissée dans la liste")
		}
	}
}

func TestLatestSessionGardeTousLesCoursDeLaSessionLaPlusRecente(t *testing.T) {
	gardes := classroom.LatestSession([]classroom.Classroom{
		groupe("a26", "5n6", "01", cohorte),
		groupe("h27", "5n6", "01", cohorte),
		groupe("h27", "4w6", "02", cohorte),
		groupe("e26", "5n6", "01", cohorte),
	})
	places := make([]string, 0, len(gardes))
	for _, cours := range gardes {
		places = append(places, cours.Scope())
	}
	// Tous les cours de l'hiver 2027, et rien des sessions précédentes.
	if strings.Join(places, " ") != "h27.5n6.01 h27.4w6.02" {
		t.Fatalf("places = %v", places)
	}
}

func TestLatestSessionSansGroupeNeGardeRien(t *testing.T) {
	if gardes := classroom.LatestSession(nil); len(gardes) != 0 {
		t.Fatalf("gardes = %+v", gardes)
	}
}

// Un nom court qui ne suit pas la convention n'a pas de place dans la suite des
// sessions : il passe après celles qui la suivent, et ne peut pas se faire
// passer pour la plus récente.
func TestLatestSessionIgnoreUnNomHorsConvention(t *testing.T) {
	gardes := classroom.LatestSession([]classroom.Classroom{
		groupe("session2026", "5n6", "01", cohorte),
		groupe("a26", "5n6", "01", cohorte),
	})
	if len(gardes) != 1 || gardes[0].Session != "a26" {
		t.Fatalf("gardes = %+v", gardes)
	}
}

// Un préfixe de cours ou de session réunit les dépôts de plusieurs groupes :
// chacun se retrouve dans le sien, par la place que son nom porte, et un nom
// hors nomenclature n'appartient à aucun.
func TestUnDepotSeRetrouveDansSonGroupe(t *testing.T) {
	groupes := []classroom.Classroom{
		groupe("a26", "5n6", "01", nil),
		groupe("a26", "5n6", "02", nil),
	}
	cours, trouve := classroom.Of(groupes, "a26.5n6.02.tp1.emilie-cote")
	if !trouve || cours.Group != "02" {
		t.Errorf("Of = « %s », %v ; attendu le groupe 02", cours.Scope(), trouve)
	}
	// La casse ne change pas une place : GitHub ne la distingue pas.
	if cours, trouve := classroom.Of(groupes, "A26.5N6.01.tp1.emilie-cote"); !trouve || cours.Group != "01" {
		t.Errorf("Of = « %s », %v ; attendu le groupe 01", cours.Scope(), trouve)
	}
	for _, nom := range []string{"a26.5n6.03.tp1.emilie-cote", "tp1-emilie-cote", "a26.5n6.01.tp1"} {
		if cours, trouve := classroom.Of(groupes, nom); trouve {
			t.Errorf("« %s » est rangé dans « %s »", nom, cours.Scope())
		}
	}
}

// Le nom qu'un dépôt affiche vient de la liste du groupe, ou de son équipe :
// jamais du dernier niveau de son nom, qui n'est qu'un slug. Un slug peut être
// le compte GitHub de quelqu'un d'autre, et son profil nommerait un inconnu.
func TestUnDepotPorteLeNomDeSonEtudiantOuDeSonEquipe(t *testing.T) {
	cours := groupe("a26", "5n6", "01", personnes("Jean-Luc Picard", "jlpicard"))
	equipes := []teams.Team{{
		Short: "alpha", Session: "a26", Course: "5n6", Group: "01",
		Members: []string{"jlpicard"},
	}}

	if nom := cours.NameOf("a26.5n6.01.tp1.jean-luc-picard", equipes); nom != "Jean-Luc Picard" {
		t.Errorf("dépôt de Jean-Luc : %q", nom)
	}
	if nom := cours.NameOf("a26.5n6.01.tp2.alpha", equipes); nom != "Équipe alpha" {
		t.Errorf("dépôt d'équipe : %q", nom)
	}
	if nom := cours.NameOf("a26.5n6.01.tp1.inconnu", equipes); nom != "" {
		t.Errorf("un dépôt hors liste prend le nom %q", nom)
	}
}
