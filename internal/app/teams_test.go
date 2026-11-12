package app_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// Les équipes au terminal. L'assistant travaille par préfixe et ignore la
// notion de groupe ; « --manage a26.5n6.01 » lui sert donc de place, et c'est
// sur elle que portent les drapeaux d'équipe.

// equipesDuGroupe rend les équipes d'un groupe parmi celles de l'organisation.
// Le faux GitHub en porte d'autres d'office — « enseignants », « direction » —,
// qui ne relèvent d'aucun groupe.
func equipesDuGroupe(h *harnais, place string) []string {
	trouvees := make([]string, 0)
	for _, nom := range h.State.TeamNames("acme") {
		if strings.HasPrefix(nom, place+".") {
			trouvees = append(trouvees, nom)
		}
	}
	return trouvees
}

// composer déclare une équipe en une commande scriptée, et rend les drapeaux
// tels qu'ils étaient : ces tests en enchaînent plusieurs.
func (h *harnais) composer(place, equipe string, membres ...string) int {
	h.t.Helper()
	avant := *h.Options
	h.Options.ManageRequested, h.Options.Manage = true, place
	h.Options.Team = []string{equipe}
	h.Options.TeamMembersOn, h.Options.TeamMembers = true, membres
	code := h.muet()
	nonInteractif := h.Options.NonInteractive
	*h.Options = avant
	h.Options.NonInteractive = nonInteractif
	return code
}

func TestTerminalComposeUneEquipe(t *testing.T) {
	h := nouveau(t, nil)
	if code := h.composer("a26.5n6.01", "eq1", "emilie-cote", "jlpicard"); code != app.ExitOK {
		t.Fatalf("code de retour %d :\n%s", code, h.texte())
	}
	if noms := equipesDuGroupe(h, "a26.5n6.01"); strings.Join(noms, ",") != "a26.5n6.01.eq1" {
		t.Fatalf("équipe attendue sur GitHub : %v", noms)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres inattendus : %v", membres)
	}
	// La liste affichée en repart : elle dit ce qui vient d'être fait.
	h.contient("Équipes de", "eq1", "@emilie-cote")
}

// Qui n'a pas encore de compte n'est dans aucune équipe, puisqu'une équipe
// réunit des comptes : la liste le dit par son nom, et non par un « @ » seul.
func TestSansEquipeNommeQuiNAPasDeCompte(t *testing.T) {
	h := nouveau(t, nil)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Naomi Chéry", StudentID: "2100456"},
		},
	})
	if code := h.composer("a26.5n6.01", "eq1", "emilie-cote"); code != app.ExitOK {
		t.Fatalf("code de retour %d :\n%s", code, h.texte())
	}
	h.contient("Sans équipe : @jlpicard, Naomi Chéry")
}

func TestTerminalDeplaceUnEtudiantDEquipe(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote", "jlpicard")
	h.composer("a26.5n6.01", "eq2", "aminata-d")

	h.Options.ManageRequested, h.Options.Manage = true, "a26.5n6.01"
	h.Options.Team = []string{"eq2"}
	h.Options.TeamAdd = []string{"jlpicard"}
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code de retour %d :\n%s", code, h.texte())
	}

	if membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1")); len(membres) != 1 {
		t.Fatalf("eq1 devrait avoir perdu Jean-Luc : %v", membres)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq2"))
	if strings.Join(membres, ",") != "aminata-d,jlpicard" {
		t.Fatalf("eq2 devrait l'avoir accueilli : %v", membres)
	}
	h.contient("quitte « eq1 »", "rejoint « eq2 »")
}

func TestTerminalRenommeEtSupprimeUneEquipe(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote")

	h.Options.ManageRequested, h.Options.Manage = true, "a26.5n6.01"
	h.Options.Team = []string{"eq1"}
	h.Options.TeamRename = "rouge"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("renommage : code %d\n%s", code, h.texte())
	}
	if noms := equipesDuGroupe(h, "a26.5n6.01"); strings.Join(noms, ",") != "a26.5n6.01.rouge" {
		t.Fatalf("équipe renommée attendue : %v", noms)
	}

	h.Options.TeamRename = ""
	h.Options.Team = []string{"rouge"}
	h.Options.TeamDelete = true
	// Supprimer demande confirmation ; en mode script, « --yes » la donne.
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("suppression : code %d\n%s", code, h.texte())
	}
	if noms := equipesDuGroupe(h, "a26.5n6.01"); len(noms) != 0 {
		t.Fatalf("l'équipe devrait avoir disparu : %v", noms)
	}
}

// Déplacer une équipe au terminal : elle, ses membres, et tout ce que les uns
// comme l'autre ont rendu. Le reste du groupe ne bouge pas.
func TestTerminalDeplaceUneEquipeVersUnAutreGroupe(t *testing.T) {
	h := nouveau(t, nil)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Aminata Diallo", Username: "aminata-d"},
		},
	})
	h.composer("a26.5n6.01", "eq1", "emilie-cote", "jlpicard")
	for _, nom := range []string{
		"a26.5n6.01.projet.eq1",
		"a26.5n6.01.tp1.emilie-cote",
		"a26.5n6.01.tp1.jean-luc-picard",
		"a26.5n6.01.tp1.aminata-diallo",
	} {
		h.State.AddRepo("acme", nom, true)
	}

	h.Options.ManageRequested, h.Options.Manage = true, "a26.5n6.01"
	h.Options.Team = []string{"eq1"}
	h.Options.TeamMove = "a26.5n6.02"
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("déplacement : code %d\n%s", code, h.texte())
	}

	attendus := []string{
		"a26.5n6.01.tp1.aminata-diallo",
		"a26.5n6.02.projet.eq1",
		"a26.5n6.02.tp1.emilie-cote",
		"a26.5n6.02.tp1.jean-luc-picard",
	}
	if noms := h.depots(); strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("dépôts après déplacement : %v", noms)
	}
	if noms := equipesDuGroupe(h, "a26.5n6.02"); strings.Join(noms, ",") != "a26.5n6.02.eq1" {
		t.Fatalf("l'équipe devrait être arrivée : %v", noms)
	}
	// Les fiches suivent : les deux membres sont dans le groupe d'arrivée, et
	// Aminata reste dans celui de départ.
	store := classroom.Open(classroom.PathNextTo(h.Reglages))
	arrivee, declare := store.Find("acme", "a26.5n6.02")
	if !declare {
		t.Fatalf("le groupe d'arrivée devrait être déclaré :\n%s", h.groupesLocaux())
	}
	if strings.Join(comptesDe(arrivee), ",") != "emilie-cote,jlpicard" {
		t.Fatalf("les membres devraient avoir suivi : %v", comptesDe(arrivee))
	}
	depart, _ := store.Find("acme", "a26.5n6.01")
	if strings.Join(comptesDe(depart), ",") != "aminata-d" {
		t.Fatalf("le départ ne devrait garder qu'Aminata : %v", comptesDe(depart))
	}
	h.contient("est maintenant", "a26.5n6.02.eq1")
}

// comptesDe rend les comptes d'un groupe, triés.
func comptesDe(cours classroom.Classroom) []string {
	liste := make([]string, 0, len(cours.Students))
	for _, personne := range cours.Students {
		liste = append(liste, personne.Username)
	}
	sort.Strings(liste)
	return liste
}

// Les dépôts ne suivent l'équipe que si on le demande : « --team-delete » seul
// les laisse, « --team-delete-repos » les emporte.
func TestTerminalSupprimeUneEquipeAvecSesDepots(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote")
	h.composer("a26.5n6.01", "eq2", "aminata-d")
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.eq1", true)
	h.State.AddRepo("acme", "a26.5n6.01.projet.eq2", true)

	h.Options.ManageRequested, h.Options.Manage = true, "a26.5n6.01"
	h.Options.Team = []string{"eq1"}
	h.Options.TeamDelete, h.Options.TeamDeleteRepos = true, true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("suppression : code %d\n%s", code, h.texte())
	}

	restants := h.State.RepoNames("acme")
	for _, nom := range restants {
		if strings.HasSuffix(nom, ".eq1") {
			t.Fatalf("les dépôts d'eq1 devaient partir : %v", restants)
		}
	}
	if !contientLe(restants, "a26.5n6.01.projet.eq2") {
		t.Fatalf("le dépôt d'eq2 devait rester : %v", restants)
	}
	h.contient("2 dépôts")
}

func contientLe(noms []string, cherche string) bool {
	for _, nom := range noms {
		if nom == cherche {
			return true
		}
	}
	return false
}

func TestTerminalAdopteUneEquipeExistante(t *testing.T) {
	state := fakegh.NewState()
	state.AddTeam("acme", "Les anciens", "emilie-cote", "jlpicard")
	h := nouveau(t, state)

	h.Options.ManageRequested, h.Options.Manage = true, "a26.5n6.01"
	h.Options.Team = []string{"eq1"}
	h.Options.TeamAdopt = "les-anciens"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("adoption : code %d\n%s", code, h.texte())
	}
	if noms := equipesDuGroupe(h, "a26.5n6.01"); strings.Join(noms, ",") != "a26.5n6.01.eq1" {
		t.Fatalf("l'équipe devrait avoir rejoint le groupe : %v", noms)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("elle garde ses membres : %v", membres)
	}
}

// Un travail d'équipe se distribue depuis la ligne de commande sans aucune
// liste : ses destinataires sont les équipes, et elles vivent sur GitHub.
func TestTerminalDistribueUnTravailDEquipe(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote")
	h.composer("a26.5n6.01", "eq2", "jlpicard", "aminata-d")

	h.Options.Assignment = "a26.5n6.01.tp1"
	h.Options.Teams = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("distribution : code %d\n%s", code, h.texte())
	}

	// Les dépôts de service en sont écartés : « .cohorte » reçoit les marques
	// des équipes, il n'est le dépôt de personne.
	noms := h.depots()
	sort.Strings(noms)
	attendus := []string{"a26.5n6.01.tp1.eq1", "a26.5n6.01.tp1.eq2"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("un dépôt par équipe attendu : %v", noms)
	}
	// L'accès va à l'équipe, pas à ses membres.
	if depots := h.State.TeamRepoNames("acme", fakegh.TeamSlug("a26.5n6.01.eq1")); len(depots) != 1 {
		t.Fatalf("eq1 devrait voir son dépôt : %v", depots)
	}
	if invites := h.State.Invitations["acme/a26.5n6.01.tp1.eq1"]; len(invites) != 0 {
		t.Fatalf("personne ne devrait être invité individuellement : %v", invites)
	}
	h.contient("Équipes à servir", "Équipe eq1")
}

func TestTerminalDistribueAQuelquesEquipes(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote")
	h.composer("a26.5n6.01", "eq2", "jlpicard")

	h.Options.Assignment = "a26.5n6.01.tp1"
	h.Options.Teams = true
	h.Options.Team = []string{"eq2"}
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("distribution : code %d\n%s", code, h.texte())
	}
	if noms := h.depots(); strings.Join(noms, ",") != "a26.5n6.01.tp1.eq2" {
		t.Fatalf("seule eq2 devait être servie : %v", noms)
	}
}

// Repartager achève l'adoption d'un travail fait en équipe avant l'outil : les
// dépôts sont déjà là et bien nommés, mais rien ne les a jamais partagés.
func TestTerminalRepartageUnTravailAdopte(t *testing.T) {
	h := nouveau(t, nil)
	h.composer("a26.5n6.01", "eq1", "emilie-cote")
	h.State.AddRepo("acme", "a26.5n6.01.tp1.eq1", true)

	h.Options.Assignment = "a26.5n6.01.tp1"
	h.Options.TeamShare = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("partage : code %d\n%s", code, h.texte())
	}
	if depots := h.State.TeamRepoNames("acme", fakegh.TeamSlug("a26.5n6.01.eq1")); len(depots) != 1 {
		t.Fatalf("le dépôt devrait être partagé avec eq1 : %v", depots)
	}
	h.contient("1 dépôt partagé avec son équipe")
}

// Une équipe appartient à un groupe : un préfixe qui n'en désigne pas un est
// refusé, avec la forme attendue dans le message.
func TestTerminalRefuseUnePlaceQuiNestPasUnGroupe(t *testing.T) {
	h := nouveau(t, nil)
	h.Options.ManageRequested, h.Options.Manage = true, "tp1"
	h.Options.Teams = true
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code attendu %d, reçu %d\n%s", app.ExitValidation, code, h.texte())
	}
	h.contient("session.cours.groupe")
}

// Repartager sans dire quel travail est refusé dès la lecture des drapeaux.
func TestTeamShareExigeUnTravail(t *testing.T) {
	sortie := &strings.Builder{}
	_, err := app.Parse([]string{"--org", "acme", "--team-share"}, sortie)
	if err == nil {
		t.Fatal("un partage sans travail devrait être refusé")
	}
	if !strings.Contains(err.Error(), "--assignment") {
		t.Fatalf("message peu explicite : %v", err)
	}
}

// Une liste écrite d'un trait se découpe sur la virgule comme sur l'espace.
func TestDrapeauxDEquipeSeLisentEnListe(t *testing.T) {
	sortie := &strings.Builder{}
	options, err := app.Parse([]string{
		"--org", "acme", "--manage", "a26.5n6.01",
		"--team", "eq1", "--team-members", "emilie-cote, jlpicard",
	}, sortie)
	if err != nil {
		t.Fatalf("lecture des drapeaux : %v", err)
	}
	if strings.Join(options.Team, ",") != "eq1" {
		t.Fatalf("équipe visée inattendue : %v", options.Team)
	}
	if !options.TeamMembersOn ||
		strings.Join(options.TeamMembers, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("composition inattendue : %v", options.TeamMembers)
	}
	// Une composition vide reste une composition : elle vide l'équipe.
	vide, err := app.Parse([]string{
		"--org", "acme", "--manage", "a26.5n6.01", "--team", "eq1", "--team-members", "",
	}, sortie)
	if err != nil {
		t.Fatalf("lecture des drapeaux : %v", err)
	}
	if !vide.TeamMembersOn || len(vide.TeamMembers) != 0 {
		t.Fatalf("« --team-members \"\" » devrait vider l'équipe : %+v", vide.TeamMembers)
	}
}

// L'assistant du terminal mène aussi aux équipes : le menu d'entrée y conduit,
// et la place se choisit parmi celles que les dépôts dessinent.
func TestAssistantMeneAuxEquipes(t *testing.T) {
	h := nouveau(t, nil)
	h.State.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)

	code, scripte := h.script(
		"equipes",              // que voulez-vous faire ?
		"a26.5n6.01",           // quel groupe ?
		"composer",             // que faire des équipes ?
		"eq1",                  // nom de l'équipe
		"emilie-cote,jlpicard", // ses membres
		"quitter",              // revenir
		"quitter",              // quitter l'outil
	)
	if code != app.ExitOK {
		t.Fatalf("code de retour %d :\n%s", code, h.texte())
	}
	if _, propose := scripte.MenuFor("Que faire des équipes"); !propose {
		t.Fatalf("le menu des équipes n'a pas été proposé :\n%s", h.texte())
	}
	if noms := equipesDuGroupe(h, "a26.5n6.01"); strings.Join(noms, ",") != "a26.5n6.01.eq1" {
		t.Fatalf("l'équipe devrait avoir été créée : %v", noms)
	}
	membres := h.State.TeamMembers("acme", fakegh.TeamSlug("a26.5n6.01.eq1"))
	if strings.Join(membres, ",") != "emilie-cote,jlpicard" {
		t.Fatalf("membres inattendus : %v", membres)
	}
}
