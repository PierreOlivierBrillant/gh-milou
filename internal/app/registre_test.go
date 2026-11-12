package app_test

import (
	"os"
	"strings"
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// La règle du dépôt veut qu'une capacité existe dans les trois interfaces. Le
// registre en est une : distribuer depuis la ligne de commande doit y inscrire
// les personnes, et l'annuaire du terminal doit les y lire — même sur une
// machine qui n'a jamais rien déclaré.

// Distribuer depuis la ligne de commande inscrit la cohorte au registre.
func TestDistributionEnLigneDeCommandeInscritAuRegistre(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.Assignment = "tp1"
	h.Options.Roster = h.cohorteCSV(
		"nom_complet,github_username",
		"Émilie Côté,emilie-cote",
		"Jean-Luc Picard,jlpicard",
	)
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	depot := state.Repos["acme/"+registry.RepoName]
	if depot == nil || !depot.Private {
		t.Fatalf("registre = %+v", depot)
	}
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	for _, attendu := range []string{"emilie-cote", "Émilie Côté", "jlpicard"} {
		if !strings.Contains(contenu, attendu) {
			t.Fatalf("« %s » manque au registre :\n%s", attendu, contenu)
		}
	}
}

// Une simulation n'écrit rien, registre compris.
func TestSimulationNInscritRienAuRegistre(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.Assignment = "tp1"
	h.Options.Roster = h.cohorteCSV("nom_complet,github_username", "Émilie Côté,emilie-cote")
	h.Options.DryRun = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if _, cree := state.Repos["acme/"+registry.RepoName]; cree {
		t.Error("une simulation ne doit rien écrire, le registre compris")
	}
}

// Un refus de confirmation non plus : rien n'est écrit sans un récapitulatif
// suivi d'un accord, et le registre ne fait pas exception.
func TestConfirmationRefuseeNInscritRienAuRegistre(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.Assignment = "tp1"
	h.Options.Roster = h.cohorteCSV()
	code, _ := h.script(
		"oui",  // Vérifier les comptes ?
		"",     // Gabarit de nom
		"",     // Dépôt modèle
		"",     // Fichiers de départ
		"",     // Visibilité
		"oui",  // Inviter ?
		"push", // Droit
		"non",  // Confirmation finale
	)
	if code != app.ExitAborted {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if _, cree := state.Repos["acme/"+registry.RepoName]; cree {
		t.Error("le registre a été écrit malgré le refus")
	}
}

// L'annuaire du terminal lit le registre : une machine qui n'a jamais rien
// déclaré y voit quand même les noms.
//
// Le registre est ici déposé tel qu'une autre machine l'aurait écrit — ou tel
// qu'on l'aurait corrigé à la main sur github.com. C'est bien le cas à
// éprouver : rien de local ne dit qui sont ces gens.
func TestAnnuaireDuTerminalLitLeRegistre(t *testing.T) {
	state := fakegh.NewState()
	for _, nom := range []string{
		"a26.5n6.01.tp1.emilie-cote", "a26.5n6.01.tp1.jean-luc-picard",
	} {
		state.AddRepo("acme", nom, true)
	}
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{
  "version": 1,
  "students": [
    {"username": "emilie-cote", "full_name": "Émilie Côté", "slugs": ["emilie-cote"]},
    {"username": "jlpicard", "full_name": "Jean-Luc Picard", "slugs": ["jean-luc-picard"]}
  ]
}`,
	}, registry.Branch)

	h := nouveau(t, state)
	h.Options.StudentsRequested = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("annuaire : code = %d\n%s", code, h.texte())
	}
	h.contient("Émilie Côté", "Jean-Luc Picard")
	h.absent("Aucun étudiant connu")
}

// ------------------------------------------------------------- publication

// Le cas de la migration : un poste qui a des années de noms, un registre vide.
func TestPublicationVerseLesNomsDuPoste(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.PublishRegistry = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	for _, attendu := range []string{"Émilie Côté", "Jean-Luc Picard", "emilie-cote"} {
		if !strings.Contains(contenu, attendu) {
			t.Fatalf("« %s » manque au registre :\n%s", attendu, contenu)
		}
	}
	h.contient("2 fiches publiées")
}

// La simulation montre tout et n'écrit rien.
func TestPublicationEnSimulationNEcritRien(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.Options.PublishRegistry = true
	h.Options.DryRun = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("Émilie Côté", "Simulation")
	if _, cree := state.Repos["acme/"+registry.RepoName]; cree {
		t.Error("une simulation ne doit rien écrire")
	}
}

// Le même compte, nommé de deux façons dans deux groupes : c'est le
// dédoublonnage demandé. La publication tranche, mais elle le dit.
func TestPublicationSignaleUnCompteNommeDeuxFois(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "h27", Course: "5n6", Group: "02",
		Students: []roster.Person{{FullName: "Emlie Côté", Username: "emilie-cote"}},
	})
	h.Options.PublishRegistry = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("ambigu", "Émilie Côté", "Emlie Côté")

	// Les deux slugs sont montés : aucun dépôt ne se détache, quel que soit le
	// nom retenu.
	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	for _, slug := range []string{"emilie-cote", "emlie-cote"} {
		if !strings.Contains(contenu, slug) {
			t.Fatalf("le slug « %s » n'est pas monté :\n%s", slug, contenu)
		}
	}
}

// Publier deux fois de suite ne réécrit rien la seconde.
func TestPublierDeuxFoisNAJouteRien(t *testing.T) {
	state := fakegh.NewState()
	premiere := nouveau(t, state)
	premiere.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	premiere.Options.PublishRegistry = true
	premiere.Options.Yes = true
	if code := premiere.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, premiere.texte())
	}
	commits := state.CallCount("POST /repos/acme/.cohorte/git/commits")

	seconde := nouveau(t, state)
	seconde.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	seconde.Options.PublishRegistry = true
	seconde.Options.Yes = true
	if code := seconde.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, seconde.texte())
	}
	seconde.contient("rien à publier")
	if apres := state.CallCount("POST /repos/acme/.cohorte/git/commits"); apres != commits {
		t.Errorf("%d commit(s) de plus pour une publication sans effet", apres-commits)
	}
}

// Un désaccord se montre et, par défaut, le registre garde son nom.
func TestPublicationMontreUnDesaccordSansTrancherSeule(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version":1,"students":[` +
			`{"username":"emilie-cote","full_name":"Émilie Côté","slugs":["emilie-cote"]}]}`,
	}, registry.Branch)

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Emilie Cote", Username: "emilie-cote"}},
	})
	h.Options.PublishRegistry = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("désaccord", "le registre garde le sien")

	contenu := state.Files("acme/"+registry.RepoName, registry.Branch)[registry.UsersFile]
	if !strings.Contains(contenu, "Émilie Côté") || strings.Contains(contenu, `"Emilie Cote"`) {
		t.Fatalf("le registre n'a pas gardé son nom :\n%s", contenu)
	}
}

// ------------------------------------------------------------- effacement

// Effacer l'historique demande de retaper le nom du dépôt. « --yes » n'y change
// rien : le mode script ne peut pas le faire du tout.
func TestEffacerLHistoriqueRefuseLeModeScript(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.ForgetRegistryHistory = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitValidation {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("retaper")
}

// Un nom approchant ne suffit pas.
func TestEffacerLHistoriqueRefuseUnNomApprochant(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{{FullName: "Émilie Côté", Username: "emilie-cote"}},
	})
	h.Options.ForgetRegistryHistory = true
	code, _ := h.script(".cohorte")
	if code != app.ExitAborted {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("intact")
}

// ------------------------------------------------------------- accès d'équipe

// Donner accès à une équipe ouvre un accès — et le dit sans laisser croire que
// cela en ferme d'autres.
func TestDonnerAccesAUneEquipe(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.RegistryTeam = "enseignants"
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	if droit := state.TeamRepos["acme/enseignants"]["acme/"+registry.RepoName]; droit == "" {
		t.Fatalf("aucun droit accordé : %+v", state.TeamRepos)
	}
	h.contient("ouvre un accès sans en fermer aucun")
}

// Une équipe inconnue se signale plutôt que de passer inaperçue.
func TestUneEquipeInconnueSeSignale(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.Options.RegistryTeam = "fantome"
	if code := h.muet(); code == app.ExitOK {
		t.Fatalf("code = %d — une équipe inconnue doit échouer\n%s", code, h.texte())
	}
}

// ------------------------------------------------------------- allègement

// Publier retire du fichier local les noms que le registre porte désormais :
// deux exemplaires d'un même nom finissent toujours par diverger.
func TestPublierAllegeLeFichierLocal(t *testing.T) {
	state := fakegh.NewState()
	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.PublishRegistry = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	local := h.groupesLocaux()
	if strings.Contains(local, "Émilie Côté") || strings.Contains(local, "Jean-Luc Picard") {
		t.Fatalf("les noms sont restés dans le fichier local :\n%s", local)
	}
	// Les comptes, eux, restent : c'est l'inscription au groupe.
	if !strings.Contains(local, "emilie-cote") || !strings.Contains(local, "jlpicard") {
		t.Fatalf("les inscriptions ont disparu :\n%s", local)
	}
	h.contient("2 noms retirés", "avant-registre")

	// La sauvegarde, elle, porte encore les noms.
	sauvegarde, err := os.ReadFile(classroom.PathNextTo(h.Reglages) + ".avant-registre")
	if err != nil {
		t.Fatalf("sauvegarde absente : %v", err)
	}
	if !strings.Contains(string(sauvegarde), "Émilie Côté") {
		t.Fatalf("la sauvegarde ne porte pas les noms :\n%s", sauvegarde)
	}
}

// Un nom que le registre n'a pas pu prendre reste écrit ici : rien ne doit se
// perdre parce qu'un désaccord n'a pas été tranché.
func TestUnNomNonRepriResteDansLeFichierLocal(t *testing.T) {
	state := fakegh.NewState()
	state.AddRepo("acme", registry.RepoName, true)
	state.SeedCommit("acme/"+registry.RepoName, map[string]string{
		registry.UsersFile: `{"version":1,"students":[` +
			`{"username":"emilie-cote","full_name":"Émilie Côté","slugs":["emilie-cote"]}]}`,
	}, registry.Branch)

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Emilie Cote", Username: "emilie-cote"}, // désaccord
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
		},
	})
	h.Options.PublishRegistry = true
	h.Options.Yes = true
	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}

	local := h.groupesLocaux()
	// Picard est monté : son nom part d'ici. Le désaccord reste, entier.
	if strings.Contains(local, "Jean-Luc Picard") {
		t.Fatalf("un nom monté est resté dans le fichier local :\n%s", local)
	}
	if !strings.Contains(local, "Emilie Cote") {
		t.Fatalf("un nom que le registre n'a pas repris a disparu :\n%s", local)
	}
}
