package registry_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// personne abrège l'écriture des cas.
func personne(nom, compte string) roster.Person {
	return roster.Person{FullName: nom, Username: compte}
}

// Apprendre quelqu'un retient d'emblée le slug que son nom produira : c'est
// celui que porteront ses dépôts.
func TestApprendreRetientLeSlugDuNom(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(),
		registry.Learn(personne("Émilie Côté", "ecote")))
	if err != nil {
		t.Fatal(err)
	}
	fiche, connue := set.Find("ecote")
	if !connue || fiche.FullName != "Émilie Côté" {
		t.Fatalf("fiche = %+v", fiche)
	}
	if len(fiche.Slugs) != 1 || fiche.Slugs[0] != "emilie-cote" {
		t.Fatalf("slugs = %v", fiche.Slugs)
	}
	// Le compte n'est pas sensible à la casse.
	if _, connue := set.Find("ECote"); !connue {
		t.Error("le compte doit se retrouver quelle que soit sa casse")
	}
}

// Le cœur de l'affaire : corriger l'orthographe d'un nom ne doit pas rendre
// orphelins les dépôts déjà créés sous l'ancien slug.
func TestCorrigerUnNomGardeLAncienSlug(t *testing.T) {
	// Le nom a d'abord été saisi avec une faute qui change le slug : les
	// premiers dépôts s'appellent « a26.5n6.01.tp1.emlie-cote ».
	set, _, err := appliquer(t, registry.Empty(),
		registry.Learn(personne("Emlie Côté", "ecote")))
	if err != nil {
		t.Fatal(err)
	}
	set, _, err = appliquer(t, set, registry.Learn(personne("Émilie Côté", "ecote")))
	if err != nil {
		t.Fatal(err)
	}

	fiche, _ := set.Find("ecote")
	if fiche.FullName != "Émilie Côté" {
		t.Fatalf("nom = %q", fiche.FullName)
	}
	// Les deux slugs mènent à elle : les dépôts d'avant la correction restent
	// rattachés, et ceux d'après le seront aussi.
	for _, slug := range []string{"emlie-cote", "emilie-cote"} {
		if trouvee, trouve := set.Resolve(slug); !trouve || trouvee.Username != "ecote" {
			t.Errorf("le slug « %s » ne mène plus à personne", slug)
		}
	}
	if len(fiche.Slugs) != 2 {
		t.Fatalf("slugs = %v : l'ancien doit être conservé", fiche.Slugs)
	}
}

// Un nom corrigé depuis la fiche garde aussi le slug que lui donnait une liste
// de groupe : le registre ne l'a peut-être jamais reçu, et c'est pourtant lui
// qui a nommé ses dépôts. Ce qu'une liste dit d'un autre compte n'y entre pas.
func TestNommerGardeLeSlugQueDonnaitUneListe(t *testing.T) {
	change := registry.Name(personne("Aleksi Lesaj", "aleksilesaj"),
		personne("Aleksi Lesa", "AleksiLesaj"), personne("Émilie Côté", "ecote"),
		personne("", "aleksilesaj"))
	set, _, err := appliquer(t, registry.Empty(), change)
	if err != nil {
		t.Fatal(err)
	}

	fiche, _ := set.Find("aleksilesaj")
	if fiche.FullName != "Aleksi Lesaj" {
		t.Fatalf("nom = %q", fiche.FullName)
	}
	if strings.Join(fiche.Slugs, ",") != "aleksi-lesa,aleksi-lesaj" {
		t.Fatalf("slugs = %v", fiche.Slugs)
	}
	if _, trouve := set.Resolve("emilie-cote"); trouve {
		t.Error("le nom d'un autre compte ne doit pas lui être attribué")
	}
}

// Adopter des dépôts hérités apprend un compte sans son nom. Cela ne doit
// jamais effacer un nom déjà connu.
func TestUnNomVideNEffaceJamaisUnNomConnu(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(),
		registry.Learn(personne("Jean-Luc Picard", "jlpicard")))
	if err != nil {
		t.Fatal(err)
	}
	set, bouge, err := appliquer(t, set, registry.Learn(personne("", "jlpicard")))
	if err != nil {
		t.Fatal(err)
	}
	if set.Name("jlpicard") != "Jean-Luc Picard" {
		t.Fatalf("nom = %q", set.Name("jlpicard"))
	}
	if bouge {
		t.Error("apprendre ce qu'on savait déjà ne doit rien changer")
	}
}

// Un dépôt adopté porte souvent le compte au lieu du nom : le registre doit
// le reconnaître aussi.
func TestLeCompteDesigneAussiLaPersonne(t *testing.T) {
	set, _, _ := appliquer(t, registry.Empty(),
		registry.Learn(personne("Jean-Luc Picard", "jlpicard")))
	if fiche, trouve := set.Resolve("jlpicard"); !trouve || fiche.FullName != "Jean-Luc Picard" {
		t.Fatalf("Resolve(compte) = %+v, %v", fiche, trouve)
	}
}

// GitHub ajoute « -1 » à un nom de dépôt déjà pris. La marque se pose à la fin,
// donc sur le slug quand c'est lui qui termine le nom : « emilie-cote-1 » reste
// le dépôt d'Émilie.
func TestLaMarqueDeDoublonNeFaitPasQuelquUnDAutre(t *testing.T) {
	set, _, _ := appliquer(t, registry.Empty(),
		registry.Learn(personne("Émilie Côté", "ecote")))
	if fiche, trouve := set.Resolve("emilie-cote-1"); !trouve || fiche.Username != "ecote" {
		t.Fatalf("Resolve = %+v, %v", fiche, trouve)
	}
	if _, trouve := set.Resolve("inconnu-1"); trouve {
		t.Error("un slug qui ne mène à personne ne doit pas être inventé")
	}
}

// Un slug appris explicitement — un dépôt adopté, nommé autrement — rattache
// lui aussi les dépôts à leur personne.
func TestUnSlugApprisRattacheLesDepots(t *testing.T) {
	set, _, _ := appliquer(t, registry.Empty(),
		registry.Learn(personne("Aminata Diallo", "aminata-d")))
	set, bouge, err := appliquer(t, set, registry.LearnSlug("aminata-d", "diallo-aminata"))
	if err != nil {
		t.Fatal(err)
	}
	if !bouge {
		t.Fatal("apprendre un slug inconnu doit changer le registre")
	}
	if fiche, trouve := set.Resolve("diallo-aminata"); !trouve || fiche.Username != "aminata-d" {
		t.Fatalf("Resolve = %+v, %v", fiche, trouve)
	}
	// Le nom n'a pas bougé : apprendre un slug n'est pas renommer.
	if set.Name("aminata-d") != "Aminata Diallo" {
		t.Errorf("nom = %q", set.Name("aminata-d"))
	}
}

// Le fichier doit se relire sur github.com : trié, indenté pareil, stable d'une
// écriture à l'autre.
func TestLeFichierEstStableEtTrie(t *testing.T) {
	set, _, _ := appliquer(t, registry.Empty(), registry.Change{Learn: []registry.User{
		registry.From(personne("Jean-Luc Picard", "jlpicard")),
		registry.From(personne("Émilie Côté", "ecote")),
		registry.From(personne("Aminata Diallo", "aminata-d")),
	}})
	premier, err := set.Encode()
	if err != nil {
		t.Fatal(err)
	}
	relu, soucis := registry.Decode(premier)
	if len(soucis) != 0 {
		t.Fatalf("relecture : %v", soucis)
	}
	second, err := relu.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(premier) != string(second) {
		t.Fatalf("l'aller-retour change le fichier :\n%s\n---\n%s", premier, second)
	}

	// Rangé par compte : les différences restent lisibles d'une version à l'autre.
	var lu struct {
		Version int              `json:"version"`
		Users   []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(premier, &lu); err != nil {
		t.Fatal(err)
	}
	// Sans renvoi d'un compte à un autre, rien n'exige la version 3.
	if lu.Version != 2 {
		t.Errorf("version écrite = %d", lu.Version)
	}
	comptes := make([]string, 0, len(lu.Users))
	for _, fiche := range lu.Users {
		comptes = append(comptes, fiche["username"].(string))
		// Le rôle est écrit pour tout le monde, « false » compris : un champ
		// absent se lirait « on ne sait pas », alors qu'on sait.
		if _, porte := fiche["is_teacher"]; !porte {
			t.Errorf("@%s : le rôle doit être écrit", fiche["username"])
		}
	}
	if strings.Join(comptes, ",") != "aminata-d,ecote,jlpicard" {
		t.Fatalf("ordre = %v", comptes)
	}
	if !strings.HasSuffix(string(premier), "\n") {
		t.Error("le fichier doit finir par un saut de ligne")
	}
}

// Quelqu'un modifiera ce fichier à la main sur github.com. Une fiche mal écrite
// doit se signaler, pas priver toute l'organisation de ses noms.
func TestUneFicheIllisibleNePrivePasDesAutres(t *testing.T) {
	contenu := []byte(`{
  "version": 1,
  "students": [
    {"username": "ecote", "full_name": "Émilie Côté"},
    {"username": "", "full_name": "Sans compte"},
    {"username": "jlpicard", "full_name": "Jean-Luc Picard"},
    {"username": "ECOTE", "full_name": "Doublon"}
  ]
}`)
	set, soucis := registry.Decode(contenu)
	if set.Len() != 2 {
		t.Fatalf("%d fiche(s) retenue(s) : %+v", set.Len(), set.All())
	}
	if len(soucis) != 2 {
		t.Fatalf("soucis = %v", soucis)
	}
	if set.Name("ecote") != "Émilie Côté" {
		t.Errorf("le doublon a écrasé la fiche d'origine : %q", set.Name("ecote"))
	}
}

// Un registre écrit par une version ultérieure de l'outil se lit quand même,
// mais le dit.
func TestUneVersionPlusRecenteSeSignale(t *testing.T) {
	set, soucis := registry.Decode([]byte(
		`{"version": 99, "students": [{"username":"ecote","full_name":"Émilie Côté"}]}`))
	if set.Len() != 1 {
		t.Fatalf("%d fiche(s)", set.Len())
	}
	if len(soucis) != 1 || !strings.Contains(soucis[0], "version 99") {
		t.Fatalf("soucis = %v", soucis)
	}
}

// Un fichier vide, absent ou illisible n'est pas une panne : c'est un registre
// qu'on n'a pas encore amorcé.
func TestUnRegistreVideNEstPasUneErreur(t *testing.T) {
	if set, _ := registry.Decode(nil); set.Len() != 0 {
		t.Error("un contenu vide doit donner un registre vide")
	}
	set, soucis := registry.Decode([]byte("{ ceci n'est pas du JSON"))
	if set.Len() != 0 || len(soucis) != 1 {
		t.Fatalf("set = %+v, soucis = %v", set.All(), soucis)
	}
}

// Une fiche sans nom complet désigne quand même quelqu'un : son compte suffit
// à dire que le dépôt est celui d'une personne connue, et non d'un slug
// orphelin. C'est ce qui permet ensuite de la nommer ou de la déplacer.
func TestUneFicheSansNomDesigneQuandMemeQuelquun(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(),
		registry.Learn(personne("", "aleksilesaj")))
	if err != nil {
		t.Fatal(err)
	}
	trouvee, connue := set.Lookup("aleksilesaj")
	if !connue {
		t.Fatal("le compte doit se retrouver même sans nom complet")
	}
	if trouvee.Username != "aleksilesaj" || trouvee.FullName != "" {
		t.Fatalf("personne = %+v", trouvee)
	}
	// La marque de doublon de GitHub n'en fait pas quelqu'un d'autre.
	if trouvee, connue := set.Lookup("aleksilesaj-1"); !connue ||
		trouvee.Username != "aleksilesaj" {
		t.Errorf("« aleksilesaj-1 » = %+v, %v", trouvee, connue)
	}
	// Ce que le registre ignore reste ignoré : un slug n'invente personne.
	if _, connue := set.Lookup("emilie-cote"); connue {
		t.Error("un slug inconnu ne doit désigner personne")
	}
}

// appliquer déroule un changement hors réseau, comme le magasin le fera.
func appliquer(t *testing.T, set *registry.Set, change registry.Change) (*registry.Set, bool, error) {
	t.Helper()
	return set.With(change, "2026-09-06")
}

// ------------------------------------------------------------------- rôles

// Le rôle ne s'apprend pas, il se décide. Une liste de classe réimportée ne
// sait pas qui enseigne : si elle pouvait l'écrire, elle ferait redescendre
// étudiant l'enseignant qui s'y trouve.
func TestApprendreQuelquunNeChangePasSonRole(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Learn(personne("Kathryn Janeway", "kjaneway")))
	if err != nil {
		t.Fatal(err)
	}
	set, _, err = appliquer(t, set, registry.SetRole("kjaneway", true))
	if err != nil {
		t.Fatal(err)
	}
	if !set.Teaches("kjaneway") {
		t.Fatal("la cooptation doit être retenue")
	}

	// Le même compte, réappris par une liste : il enseigne toujours.
	set, _, err = appliquer(t, set, registry.Learn(personne("Kathryn Janeway", "kjaneway")))
	if err != nil {
		t.Fatal(err)
	}
	if !set.Teaches("kjaneway") {
		t.Error("réapprendre quelqu'un ne doit pas lui retirer son rôle")
	}
}

// Coopter est réversible, et rejouable : deux fois le même changement donne le
// même registre, sans faux commit entre les deux.
func TestLeRoleSeRetireEtNeBougeQuUneFois(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Learn(personne("Kathryn Janeway", "kjaneway")))
	if err != nil {
		t.Fatal(err)
	}
	set, bouge, err := appliquer(t, set, registry.SetRole("kjaneway", true))
	if err != nil || !bouge {
		t.Fatalf("première cooptation : bouge=%v, err=%v", bouge, err)
	}
	if _, rebouge, _ := appliquer(t, set, registry.SetRole("kjaneway", true)); rebouge {
		t.Error("rejouer la même cooptation ne doit rien écrire")
	}
	set, bouge, err = appliquer(t, set, registry.SetRole("kjaneway", false))
	if err != nil || !bouge {
		t.Fatalf("retrait : bouge=%v, err=%v", bouge, err)
	}
	if set.Teaches("kjaneway") {
		t.Error("le rôle doit avoir été retiré")
	}
}

// On ne coopte que quelqu'un que le registre connaît : un compte inventé
// n'entre pas par la porte du rôle.
func TestOnNeCoopteQueQuelquunDeConnu(t *testing.T) {
	if _, _, err := appliquer(t, registry.Empty(), registry.SetRole("inconnu", true)); err == nil {
		t.Fatal("coopter un compte inconnu doit être refusé")
	}
}

// Teachers sert à chercher les cours qu'un collègue a donnés : il ne rend que
// ceux qui enseignent, rangés par compte.
func TestTeachersNeRendQueLesEnseignants(t *testing.T) {
	set, _, err := appliquer(t, registry.Empty(), registry.Learn(
		personne("Émilie Côté", "ecote"), personne("Kathryn Janeway", "kjaneway")))
	if err != nil {
		t.Fatal(err)
	}
	set, _, err = appliquer(t, set, registry.SetRole("kjaneway", true))
	if err != nil {
		t.Fatal(err)
	}
	enseignants := set.Teachers()
	if len(enseignants) != 1 || enseignants[0].Username != "kjaneway" {
		t.Fatalf("enseignants = %+v", enseignants)
	}
	if enseignants[0].Role() != registry.RoleTeacher {
		t.Errorf("rôle = %q", enseignants[0].Role())
	}
	fiche, _ := set.Find("ecote")
	if fiche.Role() != registry.RoleStudent {
		t.Errorf("rôle par défaut = %q", fiche.Role())
	}
}

// Une organisation amorcée par une version antérieure range ses fiches sous
// « students ». Elle ne doit pas perdre ses noms parce qu'un mot a changé.
func TestUnRegistreEnVersion1SeRelit(t *testing.T) {
	set, soucis := registry.Decode([]byte(`{
  "version": 1,
  "students": [
    {"username": "ecote", "full_name": "Émilie Côté", "slugs": ["emilie-cote"]}
  ]
}`))
	if len(soucis) != 0 {
		t.Fatalf("soucis = %v", soucis)
	}
	fiche, connu := set.Find("ecote")
	if !connu || fiche.FullName != "Émilie Côté" {
		t.Fatalf("fiche = %+v, %v", fiche, connu)
	}
	// Sans rôle écrit, personne n'enseigne : c'est ce que la version 1 disait.
	if fiche.IsTeacher {
		t.Error("une fiche de version 1 ne déclare aucun enseignant")
	}
	// Et elle se réécrit sous la clé courante.
	reecrit, err := set.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reecrit), `"users"`) ||
		strings.Contains(string(reecrit), `"students"`) {
		t.Errorf("réécriture :\n%s", reecrit)
	}
}

// Deux lignes pour un même compte sont une redondance du fichier, pas une
// intention : les écarter perdait ce que la seconde disait.
//
// C'est ce qui faisait paraître un compte sans nom alors que « etudiants.json »
// le portait — et, le nom manquant, plus rien ne rattachait ses dépôts à lui.
func TestDeuxLignesPourUnMemeCompteSeReunissent(t *testing.T) {
	set, soucis := registry.Decode([]byte(`{"version":2,"users":[
		{"username":"1600105","added_at":"2026-08-01"},
		{"username":"1600105","full_name":"Prénom Nom","slugs":["prenom-nom"],
		 "is_teacher":true,"student_id":"1600105","added_at":"2026-09-01"}
	]}`))

	if nom := set.Name("1600105"); nom != "Prénom Nom" {
		t.Errorf("Name = %q : le nom de la seconde ligne est perdu", nom)
	}
	if set.Len() != 1 {
		t.Errorf("%d fiche(s), attendu 1", set.Len())
	}
	// Le slug de la seconde ligne rattache les dépôts déjà créés sous ce nom.
	if personne, trouve := set.Lookup("prenom-nom"); !trouve ||
		personne.Username != "1600105" {
		t.Errorf("Lookup(« prenom-nom ») = %+v, %v", personne, trouve)
	}
	// Ni le rôle ni le matricule ne s'oublient.
	if !set.Teaches("1600105") {
		t.Error("le rôle déclaré sur la seconde ligne est perdu")
	}
	fiche, _ := set.Find("1600105")
	if fiche.StudentID != "1600105" {
		t.Errorf("StudentID = %q", fiche.StudentID)
	}
	// La plus ancienne date d'ajout l'emporte : c'est depuis elle qu'on connaît
	// le compte.
	if fiche.AddedAt != "2026-08-01" {
		t.Errorf("AddedAt = %q, attendu la plus ancienne", fiche.AddedAt)
	}
	// La redondance se signale quand même : le fichier gagnerait à être nettoyé.
	if len(soucis) != 1 {
		t.Errorf("soucis = %v, attendu un seul avis", soucis)
	}
}

// Deux noms qui se contredisent ne se tranchent pas en silence : le premier
// reste, et le désaccord se dit. C'est à qui relit le fichier de choisir.
func TestDeuxNomsQuiSeContredisentSeSignalent(t *testing.T) {
	set, soucis := registry.Decode([]byte(`{"version":2,"users":[
		{"username":"ecote","full_name":"Émilie Côté"},
		{"username":"ECOTE","full_name":"Emilie Cote-Tremblay","slugs":["emilie-cote-tremblay"]}
	]}`))
	if nom := set.Name("ecote"); nom != "Émilie Côté" {
		t.Errorf("Name = %q : le second nom a gagné", nom)
	}
	if len(soucis) != 1 || !strings.Contains(soucis[0], "Deux noms") {
		t.Errorf("soucis = %v", soucis)
	}
	// Le slug de la seconde ligne est gardé quand même : il rattache des
	// dépôts déjà créés, et les perdre les rendrait orphelins.
	if _, trouve := set.Lookup("emilie-cote-tremblay"); !trouve {
		t.Error("le slug de la ligne écartée est perdu")
	}
}

// Une liste de paires où l'on lit « Émilie Côté » d'un côté et
// « h24.5m6.02.tp-1.ancien-eleve » de l'autre est une liste qu'il faut
// déchiffrer ligne à ligne.
func TestNameForRendUnNomLisibleMemeSansFiche(t *testing.T) {
	set, _, err := registry.Empty().With(registry.Learn(
		roster.Person{FullName: "Émilie Côté", Username: "ecote"},
	), "2026-09-15")
	if err != nil {
		t.Fatalf("registre : %v", err)
	}

	if nom := set.NameFor("a26.5n6.01.tp1.emilie-cote"); nom != "Émilie Côté" {
		t.Fatalf("nom connu : %q", nom)
	}
	// Personne ne réclame ce slug : c'est lui qu'on montre, pas le dépôt entier.
	if nom := set.NameFor("h24.5m6.02.tp-1.ancien-eleve"); nom != "ancien-eleve" {
		t.Fatalf("nom inconnu : %q", nom)
	}
	// Un dépôt hors nomenclature garde son nom : il n'y a rien à en tirer.
	if nom := set.NameFor("notes-du-cours"); nom != "notes-du-cours" {
		t.Fatalf("hors nomenclature : %q", nom)
	}
	var absent *registry.Set
	if nom := absent.NameFor("a26.5n6.01.tp1.emilie-cote"); nom != "emilie-cote" {
		t.Fatalf("sans registre : %q", nom)
	}
}
