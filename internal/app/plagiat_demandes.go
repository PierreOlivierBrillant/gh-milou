package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/anonymize"
	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/complete"
	"github.com/PierreOlivierBrillant/gh-milou/internal/corpus"
	"github.com/PierreOlivierBrillant/gh-milou/internal/exchange"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plagiarism"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Demander à voir une copie, et décider de la montrer — au terminal.
//
// Le dépistage par index s'arrête là où il devient utile : on voit qu'une paire
// sort du lot, et rien de plus. Pour juger, il faut lire les passages communs,
// donc du code d'en face.
//
// Il ne se lit pas tout seul. Le demandeur dépose une demande nommée ; le
// propriétaire la voit et décide. S'il accorde, l'outil lui prépare l'envoi
// anonymisé de cette seule copie, sous le jeton qu'elle portait déjà — et c'est
// lui qui l'envoie. Rien ne traîne dans l'organisation à la vue de toute
// l'équipe, et rien ne part dans son dos.

// demandes ouvre l'écran des demandes depuis l'assistant.
func (m *manageSession) demandes() error {
	for {
		demandes, set, err := m.session.lireDemandes()
		if err != nil {
			return err
		}
		recues := demandes.For(m.session.Viewer)
		faites := demandes.By(m.session.Viewer)
		montrerDemandes(m.session.Console, set, recues, faites)

		choix := ui.Options("deposer", "Déposer une demande pour une copie d'un collègue")
		if len(demandes.Waiting(m.session.Viewer)) > 0 {
			choix = append(ui.Options(
				"accorder", "Accorder une demande, et préparer l'envoi",
				"refuser", "Refuser une demande",
			), choix...)
		}
		choix = append(choix, ui.Options("retour", "Retour")...)

		action, err := m.session.Prompt.Choose("Que faire ?", choix, "retour")
		if err != nil || action == "retour" {
			return err
		}
		if action == "deposer" {
			err = m.deposerDemande()
		} else {
			err = m.deciderDemande(demandes.Waiting(m.session.Viewer),
				action == "accorder")
		}
		// Un refus de l'outil — pas de copie dépistée, table d'index perdue —
		// se dit et laisse l'écran ouvert : en sortir obligerait à refaire tout
		// le chemin pour la demande suivante.
		if valid.IsValidation(err) {
			m.session.Console.Failure("%v", err)
			continue
		}
		if err != nil {
			return err
		}
	}
}

// lireDemandes relit le registre : une décision prise ailleurs depuis
// l'ouverture de la séance doit se voir ici.
func (s *Session) lireDemandes() (exchange.Asks, *registry.Set, error) {
	set, avis := s.names(s.Settings.Org)
	if avis != "" {
		s.Console.Print(s.Console.Warn(avis))
	}
	if set == nil {
		return exchange.Asks{}, nil, valid.Errorf(
			"Demandes : le registre de « %s » n'a pas pu être lu.", s.Settings.Org)
	}
	return set.Asks(), set, nil
}

// montrerDemandes affiche les deux listes : ce qu'on nous demande, et ce qu'on
// a demandé. Elles n'attendent pas la même chose — l'une une décision, l'autre
// une réponse.
func montrerDemandes(console *ui.Console, set *registry.Set,
	recues, faites []exchange.Ask) {

	console.Heading("Demandes reçues")
	if len(recues) == 0 {
		console.Note("Aucune. Un collègue qui a mesuré une de vos copies sans " +
			"pouvoir la lire en déposera une ici.")
	} else {
		console.Table([]string{"Demande", "De", "Travail", "Copie", "Mesuré",
			"État", "Ce qui est dit"}, lignesDeDemandes(set, recues, true), 0)
	}

	console.Heading("Demandes faites")
	if len(faites) == 0 {
		console.Note("Aucune.")
		return
	}
	console.Table([]string{"Demande", "À", "Travail", "Copie", "Mesuré",
		"État", "Ce qui est dit"}, lignesDeDemandes(set, faites, false), 0)
	console.Note("Une demande accordée ne fait rien apparaître ici : le " +
		"propriétaire vous envoie l'archive, que « Comparer les copies » verse dans " +
		"l'analyse, sous « Copies reçues » (ou « --import-zip »).")
}

// lignesDeDemandes met des noms sur les comptes : un tableau qui ne parlerait
// qu'en identifiants obligerait à traduire chaque ligne.
func lignesDeDemandes(set *registry.Set, demandes []exchange.Ask,
	recues bool) [][]string {

	lignes := make([][]string, 0, len(demandes))
	for _, demande := range demandes {
		compte := demande.To
		if recues {
			compte = demande.From
		}
		qui := "@" + compte
		if nom := set.Name(compte); nom != "" {
			qui = nom
		}
		mesure := ""
		if demande.Similarity > 0 {
			mesure = fmt.Sprintf("%.0f %%", demande.Similarity*100)
		}
		dit := demande.Note
		if demande.Reason != "" {
			dit = demande.Reason
		}
		lignes = append(lignes, []string{demande.ID, qui, demande.Assignment,
			demande.Token, mesure, demande.State, abrege(dit)})
	}
	return lignes
}

// deciderDemande demande laquelle, puis tranche.
func (m *manageSession) deciderDemande(attente []exchange.Ask, accorder bool) error {
	set, _ := m.session.names(m.org)
	choix := make([]ui.Option, 0, len(attente))
	for _, demande := range attente {
		qui := "@" + demande.From
		if nom := set.Name(demande.From); nom != "" {
			qui = nom
		}
		choix = append(choix, ui.Option{Value: demande.ID,
			Label: fmt.Sprintf("%s — %s, copie %s, de %s",
				demande.ID, demande.Assignment, demande.Token, qui)})
	}
	id, err := m.session.Prompt.Choose("Quelle demande ?", choix, attente[0].ID)
	if err != nil {
		return err
	}
	demande, trouvee := exchange.Asks{Asks: attente}.Find(id)
	if !trouvee {
		return valid.Errorf("Demande « %s » : inconnue.", id)
	}

	if !accorder {
		motif, err := m.session.Prompt.Ask(ui.Question{
			Title: "Ce que vous répondez", AllowEmpty: true})
		if err != nil {
			return err
		}
		return m.session.refuser(demande, motif)
	}
	destination, err := m.session.Prompt.Ask(ui.Question{
		Title:    "Fichier ZIP à écrire",
		Default:  "demande-" + strings.ToLower(demande.ID) + ".zip",
		Complete: complete.Path,
	})
	if err != nil {
		return err
	}
	return m.session.accorder(demande, destination)
}

// deposerDemande dépose une demande pour une copie mesurée mais illisible.
//
// Les jetons ne se devinent pas : ils viennent du dernier rapport, où les
// copies d'un index publié apparaissent sans nom. Sans rapport sous la main, il
// n'y a rien à demander.
func (m *manageSession) deposerDemande() error {
	console := m.session.Console
	candidates := copiesDepistees(anciensRapports(m.session.Options.ReportDir))
	if len(candidates) == 0 {
		return valid.Errorf(
			"Aucune copie dépistée dans les rapports de « %s » : comparez d'abord "+
				"vos copies à l'index publié d'un collègue (« Comparer les copies », "+
				"ou « --against »).",
			m.session.Options.ReportDir)
	}

	choix := make([]ui.Option, 0, len(candidates))
	for _, copie := range candidates {
		choix = append(choix, ui.Option{Value: copie.clef(),
			Label: fmt.Sprintf("%s — copie %s, mesurée à %.0f %%",
				copie.Assignment, copie.Token, copie.Similarity*100)})
	}
	clef, err := m.session.Prompt.Choose("Quelle copie ?", choix, candidates[0].clef())
	if err != nil {
		return err
	}
	var copie depistee
	for _, candidate := range candidates {
		if candidate.clef() == clef {
			copie = candidate
		}
	}
	mot, err := m.session.Prompt.Ask(ui.Question{
		Title: "Ce que vous dites au collègue", AllowEmpty: true})
	if err != nil {
		return err
	}

	demande, err := m.session.deposer(copie.Assignment, copie.Token,
		copie.Similarity, mot)
	if err != nil {
		return err
	}
	console.Success("Demande %s déposée auprès de @%s.", demande.ID, demande.To)
	console.Note("Il la verra en ouvrant l'outil. S'il accorde, il vous enverra " +
		"l'archive anonymisée de cette seule copie — le voile ne se lève pas tout " +
		"seul.")
	return nil
}

// depistee est une copie qu'on a mesurée sans pouvoir la lire.
type depistee struct {
	Assignment string
	Token      string
	Similarity float64
}

func (d depistee) clef() string { return d.Assignment + ":" + d.Token }

// copiesDepistees relève, dans les rapports de ce poste, les copies venues d'un
// index publié qui ressemblent le plus aux nôtres.
func copiesDepistees(rapports []*plagiarism.Report) []depistee {
	fortes := make(map[string]depistee, 8)
	for _, rapport := range rapports {
		if rapport == nil {
			continue
		}
		depistees := rapport.Screening()
		if len(depistees) == 0 {
			continue
		}
		for _, paire := range rapport.Result.Matches {
			for _, id := range []string{paire.Left, paire.Right} {
				travail, mesuree := depistees[id]
				if !mesuree {
					continue
				}
				copie := depistee{Assignment: travail, Token: id,
					Similarity: paire.Similarity}
				if ancienne, vue := fortes[copie.clef()]; vue &&
					ancienne.Similarity >= copie.Similarity {
					continue
				}
				fortes[copie.clef()] = copie
			}
		}
	}

	candidates := make([]depistee, 0, len(fortes))
	for _, copie := range fortes {
		candidates = append(candidates, copie)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Similarity != candidates[j].Similarity {
			return candidates[i].Similarity > candidates[j].Similarity
		}
		return candidates[i].clef() < candidates[j].clef()
	})
	return candidates
}

// ------------------------------------------------------------- les décisions

// deposer inscrit une demande au registre.
func (s *Session) deposer(assignment, token string, similarity float64,
	note string) (exchange.Ask, error) {

	registre, avis := s.names(s.Settings.Org)
	if avis != "" {
		s.Console.Print(s.Console.Warn(avis))
	}
	if registre == nil {
		return exchange.Ask{}, valid.Errorf(
			"Demande : le registre de « %s » n'a pas pu être lu.", s.Settings.Org)
	}
	// À qui s'adresser : c'est le catalogue qui le dit, et lui seul. Sans
	// annonce, rien ne dit à qui la copie appartient, et la demande n'irait
	// nulle part.
	ligne, annonce := registre.Catalog().Find(assignment)
	if !annonce {
		return exchange.Ask{}, valid.Errorf(
			"Demande : « %s » n'est pas annoncé au catalogue de « %s ». Sans cela, "+
				"rien ne dit à qui la copie appartient.", assignment, s.Settings.Org)
	}
	id, err := exchange.NewAskID()
	if err != nil {
		return exchange.Ask{}, err
	}
	demande := exchange.Ask{
		ID: id, From: s.Viewer, To: ligne.Teacher, Assignment: ligne.ID(),
		Token: token, Similarity: similarity, Note: strings.TrimSpace(note),
		State: exchange.AskPending, CreatedAt: s.Now().Format(time.RFC3339),
	}
	if _, err := demande.Validated(); err != nil {
		return exchange.Ask{}, err
	}
	if _, err := s.registryOf(s.Settings.Org).Apply(registry.AskFor(demande)); err != nil {
		return exchange.Ask{}, err
	}
	return demande, nil
}

// refuser inscrit un refus, avec ce qu'on répond.
func (s *Session) refuser(demande exchange.Ask, motif string) error {
	demande.State = exchange.AskDenied
	demande.DecidedAt = s.Now().Format(time.RFC3339)
	demande.Reason = strings.TrimSpace(motif)
	if _, err := s.registryOf(s.Settings.Org).Apply(registry.AskFor(demande)); err != nil {
		return err
	}
	s.Console.Success("Demande %s refusée.", demande.ID)
	return nil
}

// accorder prépare l'envoi de la seule copie demandée, puis inscrit la
// décision.
func (s *Session) accorder(demande exchange.Ask, destination string) error {
	console := s.Console
	chemin, err := cheminDArchive(destination,
		"demande-"+strings.ToLower(demande.ID))
	if err != nil {
		return err
	}
	// Quelle copie le jeton désigne : la table écrite à la publication le dit,
	// et elle seule. Perdue, personne ne peut plus répondre — et il vaut mieux
	// le dire que d'envoyer la mauvaise.
	trouvee, err := plagiarism.LocateToken(s.Options.ReportDir,
		demande.Assignment, demande.Token)
	if err != nil {
		return err
	}
	requete, err := s.requeteDuTravail(demande.Assignment)
	if err != nil {
		return err
	}

	console.Heading("Demande " + demande.ID + " — " + demande.Assignment)
	console.Note("Une seule copie part, sous le jeton « %s » que le collègue a "+
		"mesuré. Ni les autres copies, ni les noms.", demande.Token)
	envoi, err := plagiarism.Grant(s.Client, demande, trouvee,
		requete, anonymize.Options{Parts: s.Options.AnonymizeParts})
	if err != nil {
		return err
	}
	montrerEnvoi(console, envoi)
	if envoi.Copies == 0 {
		return valid.Errorf("Demande %s : la copie n'a pas pu être lue.", demande.ID)
	}
	if !s.Options.Yes {
		suite, err := s.Prompt.Confirm("Accorder, et écrire l'archive ?", true)
		if err != nil || !suite {
			return err
		}
	}
	if err := ecrireEnvoi(console, chemin, envoi); err != nil {
		return err
	}

	// La décision n'est inscrite qu'une fois l'archive écrite : un envoi qu'on
	// n'a pas su produire ne doit pas laisser une demande marquée « accordée ».
	demande.State = exchange.AskGranted
	demande.DecidedAt = s.Now().Format(time.RFC3339)
	if _, err := s.registryOf(s.Settings.Org).Apply(registry.AskFor(demande)); err != nil {
		return err
	}
	console.Success("Demande %s accordée.", demande.ID)
	console.Note("Envoyez « %s » à @%s : l'outil ne le fait pas pour vous, et "+
		"c'est voulu.", chemin, demande.From)
	return nil
}

// travailOuvert ouvre un travail nommé par son identifiant, sans passer par le
// choix de groupe de l'assistant : les drapeaux et les demandes le désignent
// déjà en entier.
func (s *Session) travailOuvert(assignment string) (
	*manageSession, classroom.Classroom, *groups.Group, error) {

	place, _, reconnu := naming.SplitAssignment(assignment)
	if !reconnu {
		return nil, classroom.Classroom{}, nil, valid.Errorf(
			"« %s » n'est pas un travail de la nomenclature.", assignment)
	}
	manager := newManageSession(s, place)
	repos, err := manager.loadRepos(false)
	if err != nil {
		return nil, classroom.Classroom{}, nil, err
	}
	groupe := groups.Build(assignment, repos)
	cours, _, connu := manager.travail(&groupe)
	if !connu {
		return nil, classroom.Classroom{}, nil, valid.Errorf(
			"« %s » n'appartient à aucun groupe de cette organisation.", assignment)
	}
	return manager, cours, &groupe, nil
}

// requeteDuTravail rebâtit la demande d'analyse d'un travail à soi, pour en
// tirer l'envoi d'une seule copie.
func (s *Session) requeteDuTravail(assignment string) (plagiarism.Request, error) {
	manager, cours, _, err := s.travailOuvert(assignment)
	if err != nil {
		return plagiarism.Request{}, err
	}
	_, nom, _ := naming.SplitAssignment(assignment)
	declarees, err := manager.reglesDeComparaison()
	if err != nil {
		return plagiarism.Request{}, err
	}
	cibles, err := manager.plagiatCibles(cours, nom, corpus.ReachGroup, declarees)
	if err != nil {
		return plagiarism.Request{}, err
	}
	reglage, err := s.Options.inspection()
	if err != nil {
		return plagiarism.Request{}, err
	}
	return plagiarism.Request{
		Assignment: assignment, Org: s.Settings.Org, Targets: cibles,
		Inspection: reglage, Rules: declarees,
		Kgram: s.Options.Kgram, Window: s.Options.Window, Jobs: s.Options.Jobs,
	}, nil
}

// ------------------------------------------------------- la ligne de commande

// demandesMode exécute ce que les drapeaux demandent des demandes : les lister,
// en déposer une, en accorder ou en refuser une.
func (s *Session) demandesMode() (int, error) {
	if id := strings.TrimSpace(s.Options.Deny); id != "" {
		demande, err := s.demandeATrancher(id)
		if err != nil {
			return ExitValidation, err
		}
		if err := s.refuser(demande, s.Options.Reason); err != nil {
			return ExitFailure, err
		}
		return ExitOK, nil
	}
	if id := strings.TrimSpace(s.Options.Grant); id != "" {
		demande, err := s.demandeATrancher(id)
		if err != nil {
			return ExitValidation, err
		}
		destination := strings.TrimSpace(s.Options.ExportZip)
		if destination == "" {
			destination = "demande-" + strings.ToLower(demande.ID) + ".zip"
		}
		if err := s.accorder(demande, destination); err != nil {
			if valid.IsValidation(err) {
				return ExitValidation, err
			}
			return ExitFailure, err
		}
		return ExitOK, nil
	}
	if cible := strings.TrimSpace(s.Options.Ask); cible != "" {
		travail, jeton, coupe := strings.Cut(cible, ":")
		if !coupe || strings.TrimSpace(jeton) == "" {
			return ExitValidation, valid.Errorf(
				"--ask : donnez le travail et la copie — « a26.5n6.02.tp1:K7DM2X ».")
		}
		demande, err := s.deposer(strings.TrimSpace(travail),
			strings.TrimSpace(jeton), 0, s.Options.Reason)
		if err != nil {
			return ExitValidation, err
		}
		s.Console.Success("Demande %s déposée auprès de @%s.", demande.ID, demande.To)
		s.Console.Note("S'il accorde, il vous enverra l'archive anonymisée de " +
			"cette seule copie ; « --import-zip » la versera dans l'analyse.")
		return ExitOK, nil
	}

	demandes, set, err := s.lireDemandes()
	if err != nil {
		return ExitValidation, err
	}
	montrerDemandes(s.Console, set, demandes.For(s.Viewer), demandes.By(s.Viewer))
	if attente := len(demandes.Waiting(s.Viewer)); attente > 0 {
		s.Console.Note("%s votre décision : « --grant ID --export-zip envoi.zip », ou "+
			"« --deny ID --reason \"…\" ».", valid.Plural(attente, "demande attend", "demandes attendent"))
	}
	return ExitOK, nil
}

// demandeATrancher retrouve une demande, et refuse d'y toucher quand elle ne
// s'adresse pas à celui qui la tranche ou qu'elle est déjà tranchée.
//
// Ce n'est pas cette vérification qui protège quoi que ce soit — le registre est
// ouvert à toute l'équipe enseignante. Elle évite qu'on tranche par mégarde une
// demande qui ne nous regarde pas.
func (s *Session) demandeATrancher(id string) (exchange.Ask, error) {
	demandes, _, err := s.lireDemandes()
	if err != nil {
		return exchange.Ask{}, err
	}
	demande, trouvee := demandes.Find(id)
	if !trouvee {
		return exchange.Ask{}, valid.Errorf("Demande « %s » : inconnue.", id)
	}
	if !strings.EqualFold(demande.To, s.Viewer) {
		return exchange.Ask{}, valid.Errorf(
			"Demande %s : elle s'adresse à @%s, pas à vous.", demande.ID, demande.To)
	}
	if !demande.Pending() {
		return exchange.Ask{}, valid.Errorf(
			"Demande %s : elle est déjà %s.", demande.ID, demande.State)
	}
	return demande, nil
}
