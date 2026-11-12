package classroom_test

import (
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// ------------------------------------------------------- renommer une fiche

func TestRenommerUnEtudiantNeTouchePasAuxAutres(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)

	modifie, err := cours.Rename("emilie-cote",
		roster.Person{FullName: "Émilie Côté-Tremblay", Username: "emilie-cote"})
	if err != nil {
		t.Fatalf("renommage refusé : %v", err)
	}
	if len(modifie.Students) != len(cohorte) {
		t.Fatalf("liste après renommage : %+v", modifie.Students)
	}
	corrigee, inscrite := modifie.Find("emilie-cote")
	if !inscrite || corrigee.FullName != "Émilie Côté-Tremblay" {
		t.Fatalf("fiche corrigée : %+v (%v)", corrigee, inscrite)
	}
	// La liste d'origine n'a pas bougé : le groupe est une valeur, pas une
	// référence partagée.
	ancienne, _ := cours.Find("emilie-cote")
	if ancienne.FullName != "Émilie Côté" {
		t.Fatalf("la liste d'origine a été modifiée : %+v", ancienne)
	}
	if voisin, _ := modifie.Find("jlpicard"); voisin.FullName != "Jean-Luc Picard" {
		t.Fatalf("un voisin a changé : %+v", voisin)
	}
}

func TestRenommerChangeLeCompte(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)

	modifie, err := cours.Rename("jlpicard",
		roster.Person{FullName: "Jean-Luc Picard", Username: "jl-picard"})
	if err != nil {
		t.Fatalf("renommage refusé : %v", err)
	}
	if _, encore := modifie.Find("jlpicard"); encore {
		t.Fatal("l'ancien compte est resté dans la liste")
	}
	if _, inscrit := modifie.Find("jl-picard"); !inscrit {
		t.Fatalf("nouveau compte absent : %+v", modifie.Students)
	}
}

func TestRenommerRefuseUnCompteDejaPris(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)

	_, err := cours.Rename("emilie-cote",
		roster.Person{FullName: "Émilie Côté", Username: "jlpicard"})
	if err == nil || !strings.Contains(err.Error(), "jlpicard") {
		t.Fatalf("erreur attendue sur le compte déjà pris : %v", err)
	}
}

func TestRenommerRefuseQuelquUnQuiNEstPasLa(t *testing.T) {
	cours := groupe("a26", "5n6", "01", cohorte)

	if _, err := cours.Rename("inconnu",
		roster.Person{FullName: "Personne", Username: "inconnu"}); err == nil {
		t.Fatal("renommer un absent aurait dû être refusé")
	}
}

// ------------------------------------------------- renommer aussi ses dépôts

func TestPlanDeRenommageSuitLeNomCorrige(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp2.emilie-cote",
		"a26.5n6.01.tp1.jean-luc-picard",
	)
	cours := groupe("a26", "5n6", "01", cohorte)
	avant, _ := cours.Find("emilie-cote")

	lignes, err := classroom.PlanRenameStudent(cours, avant,
		roster.Person{FullName: "Émilie Côté-Tremblay", Username: "emilie-cote"}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 2 {
		t.Fatalf("dépôts à renommer : %+v", lignes)
	}
	cibles := map[string]string{}
	for _, ligne := range lignes {
		cibles[ligne.Repo] = ligne.Target
	}
	if cibles["a26.5n6.01.tp1.emilie-cote"] != "a26.5n6.01.tp1.emilie-cote-tremblay" ||
		cibles["a26.5n6.01.tp2.emilie-cote"] != "a26.5n6.01.tp2.emilie-cote-tremblay" {
		t.Fatalf("cibles composées : %v", cibles)
	}
}

// Changer le seul compte ne touche pas aux noms de dépôts : ils portent le nom
// de la personne, pas son compte.
func TestPlanDeRenommageVideQuandLeNomNeChangePas(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote")
	cours := groupe("a26", "5n6", "01", cohorte)
	avant, _ := cours.Find("emilie-cote")

	lignes, err := classroom.PlanRenameStudent(cours, avant,
		roster.Person{FullName: "Émilie Côté", Username: "e-cote"}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 0 {
		t.Fatalf("rien n'aurait dû être renommé : %+v", lignes)
	}
}

func TestPlanDeRenommageRefuseUneCollision(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard",
	)
	cours := groupe("a26", "5n6", "01", cohorte)
	avant, _ := cours.Find("emilie-cote")

	_, err := classroom.PlanRenameStudent(cours, avant,
		roster.Person{FullName: "Jean-Luc Picard", Username: "emilie-cote"}, inventaire)
	if err == nil || !strings.Contains(err.Error(), "a26.5n6.01.tp1.jean-luc-picard") {
		t.Fatalf("collision attendue : %v", err)
	}
}

// Sans nom complet, il n'y a rien à écrire au dernier niveau : le dépôt garde
// le sien plutôt que de refuser le renommage. C'est la même règle qui permet de
// déplacer un travail dont on ne connaît encore personne.
func TestPlanDeRenommageGardeLeFragmentSansNomComplet(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote")
	cours := groupe("a26", "5n6", "01", cohorte)
	avant, _ := cours.Find("emilie-cote")

	lignes, err := classroom.PlanRenameStudent(cours, avant,
		roster.Person{Username: "emilie-cote"}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 0 {
		t.Fatalf("le dépôt aurait dû garder son nom : %+v", lignes)
	}
}

// ------------------------------------------------------ corriger une fiche

// Corriger un nom garde ce que la ligne porte d'autre : son matricule et ses
// autres comptes sont à la personne, pas au nom qu'on corrige.
func TestUneCorrectionGardeMatriculeEtAutresComptes(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard")
	cours := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Émilie Côté", Username: "emilie-cote", StudentID: "2100123",
			Also: []string{"emilie-perso"}},
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
	}).Enrich(nil, inventaire)

	correction, err := classroom.PlanCorrection(cours, "emilie-cote",
		roster.Person{FullName: "Émilie Côté-Tremblay"}, true, inventaire)
	if err != nil {
		t.Fatalf("correction refusée : %v", err)
	}
	apres := correction.After
	if apres.FullName != "Émilie Côté-Tremblay" || apres.Username != "emilie-cote" ||
		apres.StudentID != "2100123" || strings.Join(apres.Also, ",") != "emilie-perso" {
		t.Fatalf("fiche corrigée : %+v", apres)
	}
	if !correction.Changed() {
		t.Error("le nom a changé : la correction doit le dire")
	}
	if len(correction.Moves) != 1 ||
		correction.Moves[0].Target != "a26.5n6.01.tp1.emilie-cote-tremblay" {
		t.Fatalf("dépôts à renommer : %+v", correction.Moves)
	}
	enregistree, _ := correction.Classroom.Find("emilie-cote")
	if enregistree.StudentID != "2100123" || !enregistree.Owns("emilie-perso") {
		t.Errorf("liste corrigée : %+v", enregistree)
	}
}

// Sans renommage demandé, la fiche change et les dépôts restent tels quels.
func TestUneCorrectionSansDepotsNeLesPlanifiePas(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote")
	cours := groupe("a26", "5n6", "01", cohorte)

	correction, err := classroom.PlanCorrection(cours, "emilie-cote",
		roster.Person{FullName: "Émilie Côté-Tremblay", Username: "e-cote"}, false, inventaire)
	if err != nil {
		t.Fatalf("correction refusée : %v", err)
	}
	if len(correction.Moves) != 0 {
		t.Fatalf("aucun dépôt n'était à renommer : %+v", correction.Moves)
	}
	if correction.After.Username != "e-cote" || correction.Before.Username != "emilie-cote" {
		t.Fatalf("avant %+v, après %+v", correction.Before, correction.After)
	}
}

// Un nom corrigé ailleurs — depuis la fiche de la personne — laisse ses dépôts
// sous l'ancien. Les renommer sans rien changer d'autre est une correction à
// part entière.
func TestRenommerLesDepotsSansChangerLaFiche(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emlie-cote")
	cours := groupe("a26", "5n6", "01", cohorte).Enrich(
		nomsConnus{"emlie-cote": {FullName: "Émilie Côté", Username: "emilie-cote"}}, inventaire)

	correction, err := classroom.PlanCorrection(cours, "emilie-cote",
		roster.Person{}, true, inventaire)
	if err != nil {
		t.Fatalf("correction refusée : %v", err)
	}
	if correction.Changed() {
		t.Error("ni le nom ni le compte n'ont changé")
	}
	if len(correction.Moves) != 1 ||
		correction.Moves[0].Target != "a26.5n6.01.tp1.emilie-cote" {
		t.Fatalf("dépôts à renommer : %+v", correction.Moves)
	}
}

// Une correction qui ne change rien est refusée, avec ou sans dépôts : la
// laisser passer ferait croire à un geste qui n'a pas eu lieu.
func TestUneCorrectionQuiNeChangeRienEstRefusee(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.emilie-cote")
	cours := groupe("a26", "5n6", "01", cohorte)
	for _, avecDepots := range []bool{false, true} {
		_, err := classroom.PlanCorrection(cours, "emilie-cote",
			roster.Person{FullName: "Émilie Côté", Username: "EMILIE-COTE"}, avecDepots, inventaire)
		if err == nil || !strings.Contains(err.Error(), "n'ont changé") {
			t.Errorf("avec dépôts = %v : refus attendu, obtenu %v", avecDepots, err)
		}
	}
	if _, err := classroom.PlanCorrection(cours, "inconnu", roster.Person{FullName: "X Y"},
		false, inventaire); err == nil || !strings.Contains(err.Error(), "n'est pas dans") {
		t.Errorf("un inconnu doit être refusé : %v", err)
	}
}

// Une liste du collège ne donne que des matricules : c'est par lui qu'on
// désigne la personne à qui l'on rattache son premier compte. Le matricule
// reste sur la fiche à côté du compte, et la ligne n'est pas dédoublée.
func TestUneCorrectionRattacheUnPremierCompteParLeMatricule(t *testing.T) {
	cours := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		{FullName: "Naomi Chéry", StudentID: "2100456"},
	})

	correction, err := classroom.PlanCorrection(cours, "2100456",
		roster.Person{Username: "nchery"}, false, nil)
	if err != nil {
		t.Fatalf("rattachement refusé : %v", err)
	}
	if correction.Before.Username != "" || correction.Before.StudentID != "2100456" {
		t.Errorf("avant : %+v", correction.Before)
	}
	apres := correction.After
	if apres.Username != "nchery" || apres.StudentID != "2100456" ||
		apres.FullName != "Naomi Chéry" {
		t.Fatalf("fiche corrigée : %+v", apres)
	}
	if !correction.Changed() {
		t.Error("un compte rattaché change la fiche : la correction doit le dire")
	}
	if len(correction.Classroom.Students) != 2 {
		t.Fatalf("liste corrigée : %+v", correction.Classroom.Students)
	}
	inscrite, trouvee := correction.Classroom.Find("nchery")
	if !trouvee || inscrite.StudentID != "2100456" {
		t.Errorf("Naomi n'est pas retrouvée sous son compte : %+v (%v)", inscrite, trouvee)
	}
}

// Deux personnes sans compte n'ont en commun qu'un compte vide. C'est le
// matricule, et lui seul, qui dit quels dépôts sont ceux de Naomi : celui de
// Claire, qui n'a pas de compte non plus, ne prend pas son nom. Rien ne
// l'aurait arrêté, puisque Naomi n'a pas de dépôt pour ce travail-là.
func TestUneCorrectionParLeMatriculeNeRenommeQueSesDepots(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.naomi-chery", "a26.5n6.01.tp2.claire-otis")
	cours := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{FullName: "Claire Otis", StudentID: "2100789"},
	}).Enrich(nil, inventaire)

	correction, err := classroom.PlanCorrection(cours, "2100456",
		roster.Person{FullName: "Naomi Chéry-Dupont", Username: "nchery"}, true, inventaire)
	if err != nil {
		t.Fatalf("correction refusée : %v", err)
	}
	if len(correction.Moves) != 1 || correction.Moves[0].Repo != "a26.5n6.01.tp1.naomi-chery" ||
		correction.Moves[0].Target != "a26.5n6.01.tp1.naomi-chery-dupont" {
		t.Fatalf("seul le dépôt de Naomi doit suivre : %+v", correction.Moves)
	}
}

// Déplacer quelqu'un qui n'a pas de compte emporte ses dépôts, et pas ceux des
// autres personnes sans compte.
func TestDeplacerParLeMatriculeNEmporteQueSesDepots(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.naomi-chery", "a26.5n6.01.tp2.claire-otis")
	depart := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{FullName: "Claire Otis", StudentID: "2100789"},
	}).Enrich(nil, inventaire)
	naomi, _ := depart.Find("2100456")

	lignes, err := classroom.PlanMove(depart, groupe("a26", "5n6", "02", nil),
		[]roster.Person{naomi}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Repo != "a26.5n6.01.tp1.naomi-chery" ||
		lignes[0].Target != "a26.5n6.02.tp1.naomi-chery" {
		t.Fatalf("seul le dépôt de Naomi doit partir : %+v", lignes)
	}
}

// ---------------------------------------------------- déplacer une personne

// Une personne inscrite deux fois — sa ligne du collège, puis son compte — n'a
// qu'un dépôt, nommé d'après la ligne qui porte son nom. Il part avec elle,
// même quand c'est par son compte qu'on la désigne.
func TestDeplacerQuelquUnInscritDeuxFoisEmporteSesDepots(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.naomi-chery")
	depart := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{Username: "nchery", StudentID: "2100456"},
	}).Enrich(nil, inventaire)
	naomi, _ := depart.Find("nchery")

	lignes, err := classroom.PlanMove(depart, groupe("a26", "5n6", "02", nil),
		[]roster.Person{naomi}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Target != "a26.5n6.02.tp1.naomi-chery" {
		t.Fatalf("son dépôt doit partir avec elle : %+v", lignes)
	}
}

// sansCompte est un groupe au lendemain d'un import du collège : Jean-Luc a
// déjà son compte, Naomi et Claire n'ont que leur matricule.
func sansCompte() []roster.Person {
	return []roster.Person{
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{FullName: "Claire Otis", StudentID: "2100789"},
	}
}

// nomsDe rend les noms d'une liste, dans son ordre.
func nomsDe(gens []roster.Person) string {
	noms := make([]string, 0, len(gens))
	for _, personne := range gens {
		noms = append(noms, personne.FullName)
	}
	return strings.Join(noms, ", ")
}

// Le matricule désigne qui n'a pas de compte : c'est lui que l'on coche. Un
// compte vide, que toutes les personnes sans compte partagent, n'en désignait
// aucune — ou toutes à la fois.
func TestChoisirParLeMatriculeNeRetientQuElle(t *testing.T) {
	depart := groupe("a26", "5n6", "01", sansCompte())

	choisies, err := depart.Movers([]string{"2100456"})
	if err != nil {
		t.Fatalf("choix refusé : %v", err)
	}
	if len(choisies) != 1 || choisies[0].FullName != "Naomi Chéry" ||
		choisies[0].StudentID != "2100456" {
		t.Fatalf("personnes choisies : %+v", choisies)
	}

	ensemble, err := depart.Movers([]string{"jlpicard", "2100789"})
	if err != nil {
		t.Fatalf("choix refusé : %v", err)
	}
	if nomsDe(ensemble) != "Jean-Luc Picard, Claire Otis" {
		t.Fatalf("personnes choisies : %+v", ensemble)
	}
}

// Deux désignations d'une même personne la choisissent une fois : elle ne part
// pas deux fois, et le décompte du déplacement ne la compte pas double.
func TestDeuxDesignationsDUnePersonneNeLaChoisissentQuUneFois(t *testing.T) {
	depart := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{Username: "nchery", StudentID: "2100456"},
	})

	choisies, err := depart.Movers([]string{"nchery", "2100456"})
	if err != nil {
		t.Fatalf("choix refusé : %v", err)
	}
	if len(choisies) != 1 || choisies[0].Username != "nchery" ||
		choisies[0].FullName != "Naomi Chéry" {
		t.Fatalf("personnes choisies : %+v", choisies)
	}
}

// Ce que le groupe ne connaît pas refuse le déplacement entier, sans prêter
// d'arobase à un matricule. Rien de désigné, rien à déplacer.
func TestChoisirQuelquUnQuiNEstPasLaEstRefuse(t *testing.T) {
	depart := groupe("a26", "5n6", "01", sansCompte())

	_, err := depart.Movers([]string{"2100456", "2100999"})
	if err == nil || err.Error() != "« 2100999 » n'est pas dans « Groupe 01 »." {
		t.Errorf("refus attendu pour le matricule inconnu : %v", err)
	}
	if _, err := depart.Movers([]string{"", " "}); err == nil ||
		err.Error() != "Aucune personne à déplacer." {
		t.Errorf("refus attendu sans personne désignée : %v", err)
	}
}

// Quitter une liste se fait par ce qui désigne : Naomi n'a aucun compte, et un
// retrait par compte la laissait au départ — tout en l'inscrivant à
// l'arrivée. Claire, qui n'en a pas non plus, reste où elle est.
func TestRetirerQuiNAQueSonMatriculeNeRetireQuElle(t *testing.T) {
	depart := groupe("a26", "5n6", "01", sansCompte())
	naomi, _ := depart.Find("2100456")

	if reste := depart.Without(naomi); nomsDe(reste.Students) != "Jean-Luc Picard, Claire Otis" {
		t.Fatalf("liste de départ : %+v", reste.Students)
	}
}

// Une personne part avec toutes ses lignes : celle de son compte, celle de son
// autre compte, celle que son matricule porte encore. En laisser une la
// laisserait inscrite au départ.
func TestRetirerUnePersonneRetireToutesSesLignes(t *testing.T) {
	depart := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Naomi Chéry", StudentID: "2100456"},
		{Username: "nchery", StudentID: "2100456"},
		{FullName: "Émilie Côté", Username: "emilie-cote", Also: []string{"emilie-perso"}},
		{Username: "emilie-perso"},
		{FullName: "Jean-Luc Picard", Username: "jlpicard"},
	})
	naomi, _ := depart.Find("nchery")
	emilie, _ := depart.Find("emilie-cote")

	reste := depart.Without(naomi, emilie)
	if len(reste.Students) != 1 || reste.Students[0].Username != "jlpicard" {
		t.Fatalf("liste de départ : %+v", reste.Students)
	}
}

// Retirer un compte ne retire que la ligne qu'il désigne. Une autre personne
// qui le compte parmi les siens reste inscrite : c'est le refus de « Rename »
// qui le dira, au lieu de la voir disparaître sans un mot.
func TestRetirerUnCompteNeRetireQueSaLigne(t *testing.T) {
	cours := groupe("a26", "5n6", "01", []roster.Person{
		{FullName: "Jean-Luc Picard", Username: "jlpicard", Also: []string{"nchery"}},
		{Username: "nchery"},
		{FullName: "Naomi Chéry", StudentID: "2100456"},
	})

	reste := cours.Without(roster.Person{Username: "NChery"})
	if nomsDe(reste.Students) != "Jean-Luc Picard, Naomi Chéry" {
		t.Fatalf("liste après retrait : %+v", reste.Students)
	}
}

// Arriver dans un groupe ne confond pas deux personnes sans compte, et n'y
// inscrit pas deux fois celle qui y figurait déjà par son matricule.
func TestArriverSansCompteNInscritQuUneFois(t *testing.T) {
	depart := groupe("a26", "5n6", "01", sansCompte())
	naomi, _ := depart.Find("2100456")
	arrivee := groupe("a26", "5n6", "02", []roster.Person{
		{FullName: "Claire Otis", StudentID: "2100789"},
	})

	apres, err := arrivee.With(naomi).Validate()
	if err != nil {
		t.Fatalf("liste d'arrivée refusée : %v", err)
	}
	if nomsDe(apres.Students) != "Claire Otis, Naomi Chéry" {
		t.Fatalf("liste d'arrivée : %+v", apres.Students)
	}
	encore, err := apres.With(naomi).Validate()
	if err != nil {
		t.Fatalf("liste d'arrivée refusée : %v", err)
	}
	if len(encore.Students) != 2 || len(encore.Identities()) != 2 {
		t.Fatalf("Naomi doit y figurer une fois : %+v", encore.Students)
	}
}

// nomsConnus tient lieu de registre : un fragment de dépôt, la personne qu'il
// désigne.
type nomsConnus map[string]roster.Person

func (n nomsConnus) Lookup(fragment string) (roster.Person, bool) {
	personne, connue := n[strings.ToLower(fragment)]
	return personne, connue
}

// ------------------------------------------------------ déplacer un travail

// sansNoms déclare un groupe dont on ne connaît que les comptes : c'est l'état
// d'un groupe repris d'ailleurs, avant que les noms complets ne soient
// retrouvés. Ses dépôts portent donc le compte au dernier niveau.
func sansNoms(session, cours, section string, comptes ...string) classroom.Classroom {
	return groupe(session, cours, section, classroom.StudentsOf(comptes))
}

// Le cas qui motive tout : aucun nom complet connu, et un travail à sortir d'un
// groupe. Les dépôts arrivent à la bonne place en gardant leur dernier niveau.
func TestDeplacerUnTravailGardeLeFragmentInconnu(t *testing.T) {
	inventaire := depots(
		"h25.5n6.02.tp1.jlpicard", "h25.5n6.02.tp1.aminata-d",
		"h25.5n6.02.tp2.emilie-cote",
	)
	depart := sansNoms("h25", "5n6", "02", "jlpicard", "aminata-d", "emilie-cote")
	arrivee := groupe("a26", "5n6", "01", nil)

	lignes, err := classroom.PlanMoveAssignments(depart, arrivee,
		[]classroom.Relocation{{ID: "h25.5n6.02.tp1"}}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	cibles := map[string]string{}
	for _, ligne := range lignes {
		cibles[ligne.Repo] = ligne.Target
	}
	if len(cibles) != 2 ||
		cibles["h25.5n6.02.tp1.jlpicard"] != "a26.5n6.01.tp1.jlpicard" ||
		cibles["h25.5n6.02.tp1.aminata-d"] != "a26.5n6.01.tp1.aminata-d" {
		t.Fatalf("cibles composées : %v", cibles)
	}
}

// Le travail peut prendre un nom au passage : c'est le seul moment où corriger
// « travail-de » ne coûte rien.
func TestDeplacerUnTravailLeRenomme(t *testing.T) {
	inventaire := depots("h25.5n6.02.travail-de.jlpicard", "h25.5n6.02.travail-de.emilie-cote")
	depart := sansNoms("h25", "5n6", "02", "jlpicard", "emilie-cote")
	arrivee := groupe("h27", "420", "02", nil)

	lignes, err := classroom.PlanMoveAssignments(depart, arrivee,
		[]classroom.Relocation{{ID: "h25.5n6.02.travail-de", Name: "Travail de session"}},
		inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 2 {
		t.Fatalf("dépôts à déplacer : %+v", lignes)
	}
	for _, ligne := range lignes {
		if !strings.HasPrefix(ligne.Target, "h27.420.02.travail-de-session.") {
			t.Fatalf("cible composée : %q", ligne.Target)
		}
	}
}

// Quand le nom complet est connu, le déplacement en profite : le dépôt arrive
// nommé comme la nomenclature le veut.
func TestDeplacerUnTravailEcritLeNomConnu(t *testing.T) {
	inventaire := depots("h25.5n6.02.tp1.jlpicard")
	depart := sansNoms("h25", "5n6", "02", "jlpicard")
	depart, err := depart.Rename("jlpicard",
		roster.Person{FullName: "Jean-Luc Picard", Username: "jlpicard"})
	if err != nil {
		t.Fatalf("fiche corrigée : %v", err)
	}
	arrivee := groupe("a26", "5n6", "01", nil)

	lignes, err := classroom.PlanMoveAssignments(depart, arrivee,
		[]classroom.Relocation{{ID: "h25.5n6.02.tp1"}}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Target != "a26.5n6.01.tp1.jean-luc-picard" {
		t.Fatalf("cible composée : %+v", lignes)
	}
	if lignes[0].Username != "jlpicard" {
		t.Fatalf("étudiant rattaché : %+v", lignes[0])
	}
}

func TestDeplacerUnTravailRefuseUneCollision(t *testing.T) {
	inventaire := depots("h25.5n6.02.tp1.jlpicard", "a26.5n6.01.tp1.jlpicard")
	depart := sansNoms("h25", "5n6", "02", "jlpicard")
	arrivee := groupe("a26", "5n6", "01", nil)

	_, err := classroom.PlanMoveAssignments(depart, arrivee,
		[]classroom.Relocation{{ID: "h25.5n6.02.tp1"}}, inventaire)
	if err == nil || !strings.Contains(err.Error(), "a26.5n6.01.tp1.jlpicard") {
		t.Fatalf("collision attendue : %v", err)
	}
}

// Les fiches suivent les dépôts qui partent ; celles dont il reste un dépôt au
// départ restent inscrites des deux côtés.
func TestLesFichesSuiventLeTravailDeplace(t *testing.T) {
	inventaire := depots(
		"h25.5n6.02.tp1.jlpicard", "h25.5n6.02.tp1.aminata-d",
		"h25.5n6.02.tp2.jlpicard",
	)
	depart := sansNoms("h25", "5n6", "02", "jlpicard", "aminata-d")
	arrivee := groupe("a26", "5n6", "01", nil)

	lignes, err := classroom.PlanMoveAssignments(depart, arrivee,
		[]classroom.Relocation{{ID: "h25.5n6.02.tp1"}}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	suivent, quittent := classroom.Followers(depart, lignes, inventaire)
	if len(suivent) != 2 {
		t.Fatalf("personnes qui suivent : %+v", suivent)
	}
	if len(quittent) != 1 || quittent[0].Username != "aminata-d" {
		t.Fatalf("personnes qui quittent : %+v", quittent)
	}
}

// Un travail déplacé n'emporte, parmi ceux qui n'ont pas de compte, que ceux
// qui l'ont rendu. Le compte vide qu'ils partagent faisait suivre Claire, qui
// n'a rendu que le travail suivant, et retenait Naomi, qui n'a plus rien au
// départ.
func TestUnTravailDeplaceNEmporteQueSesInscritsSansCompte(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.jean-luc-picard", "a26.5n6.01.tp1.naomi-chery",
		"a26.5n6.01.tp2.claire-otis",
	)
	depart := groupe("a26", "5n6", "01", sansCompte())

	lignes, err := classroom.PlanMoveAssignments(depart, groupe("a26", "5n6", "02", nil),
		[]classroom.Relocation{{ID: "a26.5n6.01.tp1"}}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	suivent, quittent := classroom.Followers(depart, lignes, inventaire)
	if nomsDe(suivent) != "Jean-Luc Picard, Naomi Chéry" {
		t.Errorf("personnes qui suivent : %s", nomsDe(suivent))
	}
	if nomsDe(quittent) != "Jean-Luc Picard, Naomi Chéry" {
		t.Errorf("personnes qui quittent : %s", nomsDe(quittent))
	}
}

// Un dépôt arrivé sous son compte GitHub reste rattaché à son étudiant : c'est
// ce qui permet de le renommer une fois le nom complet retrouvé.
func TestUnDepotNommeParLeCompteResteRattache(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard")
	cours := groupe("a26", "5n6", "01", classroom.StudentsOf([]string{"jlpicard"}))

	student, inscrit := cours.StudentOf("a26.5n6.01.tp1.jlpicard")
	if !inscrit || student.Username != "jlpicard" {
		t.Fatalf("étudiant rattaché : %+v (%v)", student, inscrit)
	}
	avant, _ := cours.Find("jlpicard")
	lignes, err := classroom.PlanRenameStudent(cours, avant,
		roster.Person{FullName: "Jean-Luc Picard", Username: "jlpicard"}, inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Target != "a26.5n6.01.tp1.jean-luc-picard" {
		t.Fatalf("cible composée : %+v", lignes)
	}
}

// ------------------------------------------------------ renommer un travail

// Le cas courant : un travail mal nommé, et rien d'autre à changer. Seul le
// niveau du travail bouge dans le nom de chaque dépôt.
func TestRenommerUnTravailNeTouchePasAuReste(t *testing.T) {
	inventaire := depots(
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jlpicard",
		"a26.5n6.01.tp2.jlpicard",
	)
	cours := groupe("a26", "5n6", "01", cohorte)

	lignes, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp1",
		"projet-final", inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	cibles := map[string]string{}
	for _, ligne := range lignes {
		cibles[ligne.Repo] = ligne.Target
	}
	if len(cibles) != 2 ||
		cibles["a26.5n6.01.tp1.emilie-cote"] != "a26.5n6.01.projet-final.emilie-cote" ||
		cibles["a26.5n6.01.tp1.jlpicard"] != "a26.5n6.01.projet-final.jlpicard" {
		t.Fatalf("cibles composées : %v", cibles)
	}
}

// Le nom saisi passe par la nomenclature : « Projet final » ne peut pas entrer
// tel quel dans un nom de dépôt.
func TestRenommerUnTravailMetLeNomEnForme(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard")
	cours := groupe("a26", "5n6", "01", nil)

	lignes, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp1",
		"Projet final", inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Target != "a26.5n6.01.projet-final.jlpicard" {
		t.Fatalf("cible composée : %+v", lignes)
	}
}

// On renomme le travail, pas les personnes : un dépôt qui porte encore un compte
// GitHub le garde, même quand le nom complet est désormais connu. Le corriger a
// sa propre opération, et mêler les deux rendrait le renommage illisible.
func TestRenommerUnTravailGardeLeNiveauDeLEtudiant(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard")
	cours := groupe("a26", "5n6", "01",
		personnes("Jean-Luc Picard", "jlpicard"))

	lignes, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp1",
		"tp2", inventaire)
	if err != nil {
		t.Fatalf("plan refusé : %v", err)
	}
	if len(lignes) != 1 || lignes[0].Target != "a26.5n6.01.tp2.jlpicard" {
		t.Fatalf("cible composée : %+v", lignes)
	}
}

// Renommer vers le nom déjà porté ne fait rien : le dire vaut mieux que de
// lancer une opération vide.
func TestRenommerUnTravailRefuseLeMemeNom(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard")
	cours := groupe("a26", "5n6", "01", nil)

	_, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp1", "tp1", inventaire)
	if err == nil || !strings.Contains(err.Error(), "porte déjà ce nom") {
		t.Fatalf("refus attendu : %v", err)
	}
}

// Un nom déjà pris par un autre travail refuse l'opération entière : la moitié
// des dépôts renommés laisserait deux travaux là où il n'y en avait qu'un.
func TestRenommerUnTravailRefuseUneCollision(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard", "a26.5n6.01.tp2.jlpicard")
	cours := groupe("a26", "5n6", "01", nil)

	_, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp1", "tp2", inventaire)
	if err == nil || !strings.Contains(err.Error(), "existe déjà") {
		t.Fatalf("collision attendue : %v", err)
	}
}

// Un travail sans dépôt n'existe pas : il n'y a rien à renommer.
func TestRenommerUnTravailInconnu(t *testing.T) {
	inventaire := depots("a26.5n6.01.tp1.jlpicard")
	cours := groupe("a26", "5n6", "01", nil)

	_, err := classroom.PlanRenameAssignment(cours, "a26.5n6.01.tp9", "tp2", inventaire)
	if err == nil || !strings.Contains(err.Error(), "Aucun dépôt") {
		t.Fatalf("refus attendu : %v", err)
	}
}
