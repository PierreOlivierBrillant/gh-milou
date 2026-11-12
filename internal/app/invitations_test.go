package app_test

import (
	"testing"

	"github.com/PierreOlivierBrillant/gh-milou/internal/app"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/fakegh"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// groupeAuxQuatreInvitations monte un travail où chaque étudiant en est à un
// point différent : Émilie est entrée dans son dépôt, Jean-Luc a encore le
// temps de répondre, le délai d'Aminata est passé, et Bruno n'a pas
// d'invitation du tout — il l'a refusée, ou elle a été annulée.
func groupeAuxQuatreInvitations(t *testing.T) *harnais {
	t.Helper()
	state := fakegh.NewState()
	state.Users["btanguay"] = "Bruno Tanguay"

	state.AddRepo("acme", "a26.5n6.01.tp1.emilie-cote", true)
	state.AddCollaborator("acme/a26.5n6.01.tp1.emilie-cote", "emilie-cote", "push")

	state.AddRepo("acme", "a26.5n6.01.tp1.jean-luc-picard", true)
	state.Invite("acme/a26.5n6.01.tp1.jean-luc-picard", "jlpicard", "push")

	state.AddRepo("acme", "a26.5n6.01.tp1.aminata-diallo", true)
	state.Invite("acme/a26.5n6.01.tp1.aminata-diallo", "aminata-d", "pull")
	state.ExpireInvitations("acme/a26.5n6.01.tp1.aminata-diallo")

	state.AddRepo("acme", "a26.5n6.01.tp1.bruno-tanguay", true)

	h := nouveau(t, state)
	h.declarer(classroom.Classroom{
		Org: "acme", Session: "a26", Course: "5n6", Group: "01",
		Students: []roster.Person{
			{FullName: "Émilie Côté", Username: "emilie-cote"},
			{FullName: "Jean-Luc Picard", Username: "jlpicard"},
			{FullName: "Aminata Diallo", Username: "aminata-d"},
			{FullName: "Bruno Tanguay", Username: "btanguay"},
		},
	})
	h.Options.ManageRequested = true
	h.Options.Manage = "a26.5n6.01.tp1"
	return h
}

func TestLaLigneDeCommandeLitSendInvitations(t *testing.T) {
	options, err := app.Parse([]string{"--send-invitations"}, nil)
	if err != nil {
		t.Fatalf("Parse : %v", err)
	}
	// Des invitations appartiennent aux dépôts d'un travail : le drapeau ouvre
	// la gestion de lui-même, comme « --plagiarism ».
	if !options.SendInvitations || !options.ManageRequested {
		t.Errorf("SendInvitations = %v, ManageRequested = %v",
			options.SendInvitations, options.ManageRequested)
	}
}

// Le drapeau envoie ce qui manque : une neuve à la place de l'invitation
// expirée, une première à qui n'en a pas. Celle qui attend encore une réponse
// n'a pas besoin d'un second courriel.
func TestDrapeauSendInvitationsEnvoieCeQuiManque(t *testing.T) {
	h := groupeAuxQuatreInvitations(t)
	attente := h.State.Invitations["acme/a26.5n6.01.tp1.jean-luc-picard"][0].ID
	h.Options.SendInvitations = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient(
		"Nouvelle invitation envoyée à @aminata-d (pull)",
		"Invitation envoyée à @btanguay (push)",
	)
	h.absent("@jlpicard")

	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.aminata-diallo"]; len(invitations) != 1 ||
		invitations[0].Expired || invitations[0].Permission != "pull" {
		t.Errorf("l'invitation d'Aminata n'a pas été renouvelée : %+v", invitations)
	}
	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.bruno-tanguay"]; len(invitations) != 1 ||
		invitations[0].Login != "btanguay" || invitations[0].Permission != "push" {
		t.Errorf("Bruno n'a pas reçu sa première invitation : %+v", invitations)
	}
	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.jean-luc-picard"]; len(invitations) != 1 ||
		invitations[0].ID != attente {
		t.Errorf("l'invitation encore valable a été touchée : %+v", invitations)
	}
}

// Avec « --dry-run », les invitations manquantes se nomment sans que rien parte.
func TestDrapeauSendInvitationsSimule(t *testing.T) {
	h := groupeAuxQuatreInvitations(t)
	h.Options.SendInvitations = true
	h.Options.DryRun = true

	if code := h.muet(); code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient("@aminata-d", "renvoi (expirée)", "@btanguay", "première invitation", "Simulation")
	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.aminata-diallo"]; len(invitations) != 1 ||
		!invitations[0].Expired {
		t.Errorf("la simulation a touché à l'invitation d'Aminata : %+v", invitations)
	}
	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.bruno-tanguay"]; len(invitations) != 0 {
		t.Errorf("la simulation a invité Bruno : %+v", invitations)
	}
}

// L'assistant montre l'état de chaque invitation dès que les accès sont lus, et
// ne propose l'envoi que sur un dépôt qu'il débloquerait.
func TestAssistantMontreEtEnvoieLesInvitationsManquantes(t *testing.T) {
	h := groupeAuxQuatreInvitations(t)
	code, _ := h.script(
		"acces", "inspecter", // relit les accès : l'état des invitations devient connu
		"liste", "rafraichir", // et la liste remontrée le porte
		"acces", "collaborateurs", "a26.5n6.01.tp1.bruno-tanguay", "envoyer", "revenir",
		"acces", "collaborateurs", "a26.5n6.01.tp1.aminata-diallo", "envoyer", "revenir",
		"quitter",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, h.texte())
	}
	h.contient(
		"aminata-d (expirée)",
		"Invitation", "acceptée", "en attente", "expirée", "sans invitation",
		"2 dépôts sans invitation valable",
		"Invitation expirée : @aminata-d",
		"Invitation envoyée à @btanguay (push)",
		"Nouvelle invitation envoyée à @aminata-d (pull)",
	)
	if invitations := h.State.Invitations["acme/a26.5n6.01.tp1.bruno-tanguay"]; len(invitations) != 1 {
		t.Errorf("Bruno n'a pas reçu sa première invitation : %+v", invitations)
	}

	// Une invitation encore valable ne se voit pas proposer d'envoi.
	suivant := nouveauDansLeMemeDossier(t, h)
	suivant.Options.ManageRequested = true
	suivant.Options.Manage = "a26.5n6.01.tp1"
	code, _ = suivant.script(
		"acces", "collaborateurs", "a26.5n6.01.tp1.jean-luc-picard", "revenir",
		"quitter",
	)
	if code != app.ExitOK {
		t.Fatalf("code = %d\n%s", code, suivant.texte())
	}
	menu, trouve := suivant.dernierMenu("Action")
	if !trouve {
		t.Fatal("aucun menu « Action »")
	}
	for _, option := range menu.Options {
		if option.Value == "envoyer" {
			t.Error("l'envoi est proposé pour une invitation encore valable")
		}
	}
}
