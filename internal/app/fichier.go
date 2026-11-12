package app

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/broadcast"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/identity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Déposer un fichier dans tous les dépôts d'un travail, ou d'un groupe entier,
// au terminal. Ce qui décide — les champs du gabarit, le chemin acceptable, le
// sort d'un fichier déjà présent — vit dans « broadcast » ; ce fichier ne fait
// que demander et montrer.

// Lignes de l'aperçu : assez pour voir que les champs se remplissent, pas au
// point de chasser la liste des dépôts de l'écran.
const lignesDApercu = 12

// planDeDepot remplit le gabarit pour chaque dépôt du groupe. Chacun reçoit les
// champs de son propre travail : un groupe entier en mêle plusieurs, et
// « {travail} » ou « {echeance} » n'y ont pas une seule valeur.
func (m *manageSession) planDeDepot(group *groups.Group, demande broadcast.Request) (
	*broadcast.Plan, error) {
	if group.Len() == 0 {
		return nil, valid.Errorf("Aucun dépôt dans « %s ».", group.Prefix)
	}
	lots := broadcast.ForRepos(group.Prefix, m.groupesDe(group), m.equipesDe, group.Repos,
		m.session.Now())
	return broadcast.PrepareAll(demande, lots)
}

// avertirDuPlan dit ce que le gabarit risque de mal faire : un champ inconnu
// — souvent une faute de frappe — ou un champ qui reste vide dans des dépôts.
func (m *manageSession) avertirDuPlan(plan *broadcast.Plan) {
	console := m.session.Console
	if !plan.Templated {
		console.Note("Contenu déposé tel quel : seuls le chemin et le message reçoivent le gabarit.")
	}
	if len(plan.Unknown) > 0 {
		console.Warning("%s : {%s}. Champs disponibles : %s.",
			valid.Agree(len(plan.Unknown), "Champ inconnu, laissé tel quel",
				"Champs inconnus, laissés tels quels"),
			strings.Join(plan.Unknown, "}, {"), broadcast.FieldList())
	}
	if incomplets := plan.Incomplete(); incomplets > 0 {
		for _, item := range plan.Items {
			if len(item.Empty) > 0 {
				console.Warning("%s un champ vide — par exemple %s : {%s}.",
					valid.Plural(incomplets, "dépôt laisse", "dépôts laissent"),
					item.Repo, strings.Join(item.Empty, "}, {"))
				break
			}
		}
	}
}

// montrerApercu montre le fichier tel que le premier dépôt le recevra.
func (m *manageSession) montrerApercu(plan *broadcast.Plan) {
	if !plan.Templated || len(plan.Items) == 0 {
		return
	}
	console := m.session.Console
	premier := plan.Items[0]
	console.Printf("  Aperçu pour %s — %s :", console.Bold(premier.Repo), premier.Path)
	lignes := strings.Split(strings.TrimRight(string(premier.Content), "\n"), "\n")
	for index, ligne := range lignes {
		if index == lignesDApercu {
			console.Printf("    %s",
				console.Dim("… "+valid.Plural(len(lignes)-lignesDApercu, "ligne")+" de plus"))
			break
		}
		console.Printf("    %s %s", console.Dim("│"), ligne)
	}
}

// deposerFichier demande le fichier et sa destination, montre ce que chaque
// dépôt deviendrait, puis dépose après confirmation.
func (m *manageSession) deposerFichier(group *groups.Group) error {
	session, console := m.session, m.session.Console
	console.Heading("Déposer un fichier dans les dépôts de « " + group.Prefix + " »")

	var source broadcast.Source
	if _, err := session.Prompt.Ask(ui.Question{
		Title:    "Fichier à déposer",
		Complete: complete.Path,
		Validate: func(value string) (string, error) {
			lu, err := broadcast.Load(value)
			if err != nil {
				return "", err
			}
			source = lu
			return value, nil
		},
	}); err != nil {
		return err
	}

	console.Note("Le contenu, le chemin et le message acceptent les champs %s.",
		broadcast.FieldList())
	chemin, err := session.Prompt.Ask(ui.Question{
		Title:    "Chemin dans le dépôt",
		Default:  source.Name,
		Validate: broadcast.CleanPath,
	})
	if err != nil {
		return err
	}
	message, err := session.Prompt.Ask(ui.Question{
		Title:      "Message du commit (vide = « Ajoute " + chemin + " »)",
		AllowEmpty: true,
	})
	if err != nil {
		return err
	}

	// Un fichier de code peut porter « {cours} » pour de bon : on ne demande
	// que s'il y a quelque chose à remplir, et remplir est le choix proposé.
	brut := false
	if champs := broadcast.Used(string(source.Content)); broadcast.IsText(source.Content) &&
		len(champs) > 0 {
		remplir, err := session.Prompt.Confirm("Le contenu emploie {"+
			strings.Join(champs, "}, {")+"} : les remplir pour chaque dépôt ?", true)
		if err != nil {
			return err
		}
		brut = !remplir
	}

	plan, err := m.planDeDepot(group, broadcast.Request{
		Content: source.Content, Path: chemin, Message: message, Raw: brut,
	})
	if err != nil {
		return err
	}
	m.avertirDuPlan(plan)
	m.montrerApercu(plan)

	// L'état de chaque dépôt est lu avant de rien écrire : c'est lui qui dit
	// où le fichier est déjà, et où il en remplacerait un autre.
	prevus := m.executerDepot(plan, broadcast.Options{DryRun: true}, "Lecture")
	m.tableauDeDepot(prevus)
	compte := broadcast.Counts(prevus)

	remplacer := false
	if compte[broadcast.Kept] > 0 {
		conserves := compte[broadcast.Kept]
		console.Note("Un fichier différent porte déjà ce nom dans %s : c'est peut-être "+
			"celui que l'étudiant a modifié.", valid.Plural(conserves, "dépôt"))
		if remplacer, err = session.Prompt.Confirm(valid.Agree(conserves,
			"Le remplacer dans ce dépôt ?",
			"Le remplacer dans ces "+itoa(conserves)+" dépôts ?"), false); err != nil {
			return err
		}
	}
	aFaire := compte[broadcast.Added] + compte[broadcast.Replaced]
	if remplacer {
		aFaire += compte[broadcast.Kept]
	}
	if aFaire == 0 {
		console.Success("Rien à déposer : chaque dépôt est à jour ou garde son fichier.")
		return nil
	}
	confirme, err := session.Prompt.Confirm(
		"Déposer le fichier dans "+valid.Plural(aFaire, "dépôt")+" ?", true)
	if err != nil || !confirme {
		return err
	}
	faits := m.executerDepot(plan, broadcast.Options{Overwrite: remplacer}, "Dépôt")
	m.bilanDeDepot(faits)
	m.relireApresDepot(faits)
	return nil
}

// relireApresDepot relit l'historique des dépôts écrits, pour que leur dernier
// envoi reste celui de l'étudiant plutôt que celui du fichier déposé.
func (m *manageSession) relireApresDepot(faits []broadcast.Result) {
	ecrits := broadcast.Written(faits)
	if len(ecrits) == 0 {
		return
	}
	progression := ui.NewProgress(m.session.Console, "Historiques", len(ecrits))
	m.resolver.Handins(m.org, ecrits, identity.Refresh,
		func(done, _ int, repo string) { progression.Update(done, repo) })
	progression.Clear()
}

// deposerDepuisDrapeaux fait ce que « --push-file » demande, sans rien
// demander : le drapeau est déjà la demande, et « --push-overwrite » dit ce
// qu'il faut faire d'un fichier différent. Le retour compte les échecs.
func (m *manageSession) deposerDepuisDrapeaux(group *groups.Group) (int, error) {
	options := m.session.Options
	source, err := broadcast.Load(options.PushFile)
	if err != nil {
		return 0, err
	}
	chemin := options.PushPath
	if strings.TrimSpace(chemin) == "" {
		chemin = source.Name
	}
	plan, err := m.planDeDepot(group, broadcast.Request{
		Content: source.Content, Path: chemin, Message: options.PushMessage, Raw: options.PushRaw,
	})
	if err != nil {
		return 0, err
	}
	console := m.session.Console
	console.Heading("Dépôt de « " + source.Name + " » dans « " + group.Prefix + " »")
	m.avertirDuPlan(plan)

	if options.DryRun {
		m.montrerApercu(plan)
		m.tableauDeDepot(m.executerDepot(plan,
			broadcast.Options{DryRun: true, Overwrite: options.PushOverwrite}, "Lecture"))
		console.Note("Simulation : rien n'a été écrit.")
		return 0, nil
	}
	faits := m.executerDepot(plan, broadcast.Options{Overwrite: options.PushOverwrite}, "Dépôt")
	m.bilanDeDepot(faits)
	m.relireApresDepot(faits)
	if kept := broadcast.Counts(faits)[broadcast.Kept]; kept > 0 {
		console.Note("« --push-overwrite » remplacerait %s.", valid.Agree(kept,
			"le fichier conservé", "les "+itoa(kept)+" fichiers conservés"))
	}
	return broadcast.Counts(faits)[broadcast.Failed], nil
}

// executerDepot passe sur chaque dépôt avec une barre d'avancement.
func (m *manageSession) executerDepot(plan *broadcast.Plan, options broadcast.Options,
	etiquette string) []broadcast.Result {
	progression := ui.NewProgress(m.session.Console, etiquette, len(plan.Items))
	options.OnResult = func(done, _ int, result broadcast.Result) {
		progression.Update(done, result.Repo)
	}
	resultats := broadcast.Run(m.session.Client, m.org, plan, options)
	progression.Clear()
	return resultats
}

// tableauDeDepot montre ce que chaque dépôt deviendrait.
func (m *manageSession) tableauDeDepot(resultats []broadcast.Result) {
	rows := make([][]string, 0, len(resultats))
	for _, resultat := range resultats {
		rows = append(rows, []string{resultat.Repo, resultat.Recipient, resultat.Path,
			m.motDeDepot(resultat.Status), resultat.Error})
	}
	m.session.Console.Table([]string{"Dépôt", "Destinataire", "Chemin", "Issue", ""}, rows, 40)
}

// bilanDeDepot dit ce qui est arrivé : les échecs un à un, le reste compté.
func (m *manageSession) bilanDeDepot(resultats []broadcast.Result) {
	console := m.session.Console
	for _, resultat := range resultats {
		if resultat.Status == broadcast.Failed {
			console.Failure("%s : %s", resultat.Repo, resultat.Error)
		}
	}
	compte := broadcast.Counts(resultats)
	console.Printf("  %s %s · %s %s · %s déjà à jour · %s %s · %s en échec",
		console.OK(itoa(compte[broadcast.Added])), valid.Agree(compte[broadcast.Added], "ajouté"),
		console.OK(itoa(compte[broadcast.Replaced])),
		valid.Agree(compte[broadcast.Replaced], "remplacé"),
		console.Info(itoa(compte[broadcast.UpToDate])),
		console.Warn(itoa(compte[broadcast.Kept])), valid.Agree(compte[broadcast.Kept], "conservé"),
		console.Err(itoa(compte[broadcast.Failed])))
}

func (m *manageSession) motDeDepot(statut broadcast.Status) string {
	console := m.session.Console
	switch statut {
	case broadcast.Added, broadcast.Replaced:
		return console.OK(string(statut))
	case broadcast.Kept:
		return console.Warn(string(statut))
	case broadcast.Failed:
		return console.Err(string(statut))
	}
	return console.Dim(string(statut))
}
