package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Rattacher au terminal les dépôts que personne ne réclame.
//
// L'annuaire est ici à l'échelle de l'organisation, et c'est le bon grain : le
// registre l'est aussi, et quelqu'un qui a suivi deux cours ne doit être
// reconnu qu'une fois. Le domaine décide de tout — ce qui est orphelin, ce que
// GitHub reconnaît, ce qu'un rattachement à la main exige — ; le terminal ne
// fait que montrer et demander.

// comptesSuivis fait voir, pendant la recherche, quel compte est demandé :
// vingt profils lus en silence passeraient pour un écran figé.
type comptesSuivis struct {
	comptes classroom.Accounts
	spin    *ui.Spinner
}

func (c comptesSuivis) Account(login string) (roster.Person, bool, error) {
	c.spin.Detail("@" + login)
	return c.comptes.Account(login)
}

// comptesGithub rend ce qui répond, pour le domaine, à « ce compte existe-t-il ».
func (d *directorySession) comptesGithub() *identity.Resolver {
	return identity.New(d.session.Client, d.session.Cache, d.session.Options.Jobs)
}

// adoptOrphans rattache les dépôts que personne ne réclame. Il demande d'abord
// à GitHub qui se cache derrière chaque dernier niveau, montre ce qu'il ferait,
// laisse écarter un compte qui porte le slug d'un nom sans être l'étudiant,
// puis écrit au registre — comme tout ce qui écrit dans cet outil.
func (d *directorySession) adoptOrphans() (int, error) {
	console := d.session.Console
	if len(d.orphans) == 0 {
		console.Success("Tous les dépôts de « %s » ont leur étudiant.", d.org)
		return ExitOK, nil
	}

	spin := ui.NewSpinner(console, "Recherche des comptes sur GitHub…")
	spin.Start()
	plan, err := classroom.PlanAdoption(d.orphans,
		comptesSuivis{comptes: d.comptesGithub(), spin: spin})
	spin.Stop()
	if err != nil {
		return ExitFailure, err
	}
	d.showAdoption(plan)

	if !plan.Ready() {
		console.Warning("Aucun de ces derniers niveaux n'est un compte GitHub.")
		return d.thenReload(d.attachUnknown(plan.Unknown))
	}
	if d.session.Options.DryRun {
		console.Blank()
		console.Note("Simulation : rien n'a été écrit.")
		return ExitOK, nil
	}

	retenu := plan
	switch {
	case d.session.Options.Yes:
	case d.session.Interactive():
		// Choisir tient lieu de confirmer : rien n'est écrit de ce qu'on
		// décoche, et tout décocher n'écrit rien.
		if retenu, err = d.chooseAdopted(plan); err != nil {
			return ExitOK, err
		}
	default:
		suite, err := d.session.Prompt.Confirm("Rattacher "+
			valid.Plural(plan.Repos(), "dépôt")+" à leur étudiant ?", false)
		if err != nil {
			return ExitValidation, err
		}
		if !suite {
			console.Warning("Annulé : rien n'a été rattaché.")
			return ExitAborted, nil
		}
	}

	if retenu.Ready() {
		if _, err := d.session.registryOf(d.org).Apply(
			registry.Attach(attachments(retenu.Found)...)); err != nil {
			return ExitFailure, err
		}
		console.Success("%s à leur étudiant, sous %s.",
			valid.Plural(retenu.Repos(), "dépôt rattaché", "dépôts rattachés"),
			valid.Plural(len(retenu.Found), "compte"))
	} else {
		console.Warning("Rien n'a été rattaché.")
	}
	return d.thenReload(d.attachUnknown(plan.Unknown))
}

// thenReload relit l'annuaire après des rattachements, une seule fois : il
// montre ensuite les étudiants retrouvés, et ce qui reste à rattacher.
func (d *directorySession) thenReload(code int, err error) (int, error) {
	if err != nil || code != ExitOK {
		return code, err
	}
	return ExitOK, d.load(false)
}

// showAdoption écrit ce que GitHub a répondu, un dernier niveau par ligne. Le
// nom du profil est ce qui permet de juger : un compte qui porte le slug d'un
// nom n'est pas forcément celui de l'étudiant.
func (d *directorySession) showAdoption(plan classroom.Adoption) {
	console := d.session.Console
	rows := make([][]string, 0, len(plan.Found)+len(plan.Unknown))
	for _, trouve := range plan.Found {
		nom := trouve.Person.FullName
		if nom == "" {
			nom = console.Dim("profil sans nom")
		}
		rows = append(rows, []string{trouve.Orphan.Fragment,
			itoa(len(trouve.Orphan.Repos)), "@" + trouve.Person.Username, nom})
	}
	for _, reste := range plan.Unknown {
		rows = append(rows, []string{reste.Fragment, itoa(len(reste.Repos)),
			console.Warn("aucun compte"), console.Dim("—")})
	}
	console.Heading(valid.Plural(len(d.orphans), "dernier niveau sans étudiant",
		"derniers niveaux sans étudiant") + " dans « " + d.org + " »")
	console.Table([]string{"Dernier niveau", "Dépôts", "Compte GitHub", "Nom du profil"},
		rows, 40)
}

// chooseAdopted laisse décocher ce qui ne doit pas se rattacher. Tout est
// coché d'avance : le cas courant est le dépôt qui porte le compte de son
// étudiant, et le faire cocher un à un ferait trente clics pour rien.
func (d *directorySession) chooseAdopted(plan classroom.Adoption) (classroom.Adoption, error) {
	options := make([]ui.Option, 0, len(plan.Found))
	coches := make([]bool, 0, len(plan.Found))
	for _, trouve := range plan.Found {
		nom := trouve.Person.FullName
		if nom == "" {
			nom = "profil sans nom"
		}
		options = append(options, ui.Option{Value: trouve.Orphan.Fragment,
			Label: trouve.Orphan.Fragment + " → @" + trouve.Person.Username + " (" + nom + ", " +
				valid.Plural(len(trouve.Orphan.Repos), "dépôt") + ")"})
		coches = append(coches, true)
	}
	indices, err := d.session.Prompt.MultiSelect("Lesquels rattacher ?", options, coches)
	if err != nil {
		return classroom.Adoption{}, err
	}
	retenus := make([]string, 0, len(indices))
	for _, index := range indices {
		retenus = append(retenus, options[index].Value)
	}
	return plan.Only(retenus), nil
}

// attachUnknown propose de rattacher à la main ce que GitHub ne reconnaît pas.
// C'est l'équivalent, dans l'assistant, du champ « Compte » que le navigateur
// offre sur la ligne d'un dépôt, et de « --attach » en script.
func (d *directorySession) attachUnknown(restes []classroom.Orphan) (int, error) {
	console := d.session.Console
	if len(restes) == 0 {
		return ExitOK, nil
	}
	if !d.session.Interactive() || d.session.Options.Yes || d.session.Options.DryRun {
		console.Note("%s : « --attach dernier-niveau=compte » les rattache un à un.",
			valid.Plural(len(restes), "dernier niveau reste sans compte",
				"derniers niveaux restent sans compte"))
		return ExitOK, nil
	}
	suite, err := d.session.Prompt.Confirm("Rattacher à la main "+
		valid.Plural(len(restes), "dernier niveau sans compte", "derniers niveaux sans compte")+
		" ?", false)
	if err != nil || !suite {
		return ExitOK, err
	}
	comptes := d.comptesGithub()
	for _, reste := range restes {
		login, err := d.session.Prompt.Ask(ui.Question{
			Title: "Compte GitHub de « " + reste.Fragment + " » (" +
				strings.Join(reste.Assignments, ", ") + ") — vide pour passer",
			AllowEmpty: true,
		})
		if err != nil {
			return ExitOK, err
		}
		if strings.TrimSpace(login) == "" {
			continue
		}
		if err := d.attach(classroom.AttachRequest{Fragment: reste.Fragment, Login: login},
			comptes); err != nil {
			console.Failure("%v", err)
		}
	}
	return ExitOK, nil
}

// attachOrphan rattache à la main un dernier niveau, comme le demande
// « --attach ».
func (d *directorySession) attachOrphan(demande classroom.AttachRequest) (int, error) {
	if err := d.attach(demande, d.comptesGithub()); err != nil {
		return ExitValidation, err
	}
	return ExitOK, nil
}

// attach vérifie puis écrit un rattachement fait à la main. L'annuaire n'est
// pas relu ici : plusieurs rattachements se suivent, et c'est à qui les
// enchaîne de le relire une fois.
func (d *directorySession) attach(demande classroom.AttachRequest,
	comptes classroom.Accounts) error {
	console := d.session.Console
	set, _ := d.session.names(d.org)
	trouve, err := classroom.PlanAttach(d.orphans, demande, set, comptes)
	if err != nil {
		return err
	}
	qui := "@" + trouve.Person.Username
	if trouve.Person.FullName != "" {
		qui = trouve.Person.FullName + " (" + qui + ")"
	}
	if d.session.Options.DryRun {
		console.Note("Simulation : « %s » — %s à %s ; rien n'a été écrit.",
			trouve.Orphan.Fragment, valid.Plural(len(trouve.Orphan.Repos),
				"dépôt serait rattaché", "dépôts seraient rattachés"), qui)
		return nil
	}
	if _, err := d.session.registryOf(d.org).Apply(
		registry.Attach(attachments([]classroom.Found{trouve})...)); err != nil {
		return err
	}
	console.Success("%s à %s.", valid.Plural(len(trouve.Orphan.Repos),
		"dépôt rattaché", "dépôts rattachés"), qui)
	return nil
}

// attachments traduit ce que le domaine a trouvé en une écriture du registre.
// C'est le seul endroit où les deux vocabulaires se rencontrent : ni l'un ni
// l'autre paquet n'a besoin de connaître l'autre.
func attachments(trouves []classroom.Found) []registry.Attachment {
	liens := make([]registry.Attachment, 0, len(trouves))
	for _, trouve := range trouves {
		liens = append(liens, registry.Attachment{
			Fragment: trouve.Orphan.Fragment, Person: trouve.Person})
	}
	return liens
}
