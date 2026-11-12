// Package plagiarism mène une analyse de bout en bout et en rend un rapport.
//
// C'est la façade que les trois interfaces appellent, et la seule : le
// navigateur, l'assistant du terminal et la ligne de commande y trouvent la
// même analyse, le même rapport et les mêmes mots. Ce qui est décidé ici — ce
// qu'on écarte, ce qu'on compare, comment on le nomme — l'est une fois.
//
// Le rapport est un fichier. C'est ce qui permet de le rouvrir sans refaire
// l'analyse, de le comparer à celui du mois dernier, et — le jour où l'analyse
// tournera dans une GitHub Action — de la faire ailleurs sans rien changer au
// moteur : l'action produit le fichier, l'interface l'ouvre.
//
// Rien de ce qui est rendu ne porte de code. Le rapport tient des empreintes,
// des décomptes et des chemins ; le texte des fichiers n'est retéléchargé que
// lorsqu'une paire est ouverte, et pour ces deux dépôts-là seulement.
package plagiarism

import (
	"fmt"
	"time"

	"github.com/PierreOlivierBrillant/gh-milou/internal/corpus"
	"github.com/PierreOlivierBrillant/gh-milou/internal/inspect"
	"github.com/PierreOlivierBrillant/gh-milou/internal/rules"
	"github.com/PierreOlivierBrillant/gh-milou/internal/similarity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Version est celle du schéma de rapport écrit. Elle est relue, jamais
// devinée : un rapport venu d'une version ultérieure doit pouvoir se signaler
// plutôt que d'être mal compris.
const Version = 1

// Disclaimer est l'avertissement que les trois interfaces affichent, sans
// exception et sans le raccourcir.
//
// Il est écrit ici parce qu'il ne doit pas exister en trois versions. Un outil
// qui mesure des ressemblances et qu'on prend pour un juge fait des dégâts
// réels sur des personnes réelles ; le dire une fois, au même endroit, est le
// minimum.
const Disclaimer = "Cet outil n'est pas un détecteur de plagiat. Il mesure des " +
	"ressemblances entre des travaux et les classe par ordre de suspicion. Une " +
	"ressemblance forte n'est pas une preuve : elle se vérifie en lisant les " +
	"passages communs, et elle s'explique parfois — un exercice très contraint, " +
	"un extrait de cours, une source commune. Une ressemblance faible ne prouve " +
	"rien non plus."

// Request est une demande d'analyse.
type Request struct {
	// Assignment nomme le travail analysé, Org l'organisation.
	Assignment string `json:"assignment,omitempty"`
	Org        string `json:"org,omitempty"`
	// Targets sont les copies à comparer.
	Targets []corpus.Target `json:"targets"`
	// Indexes nomme les travaux d'un collègue dont l'index publié entre dans le
	// corpus. Ce sont des empreintes, jamais du code : le cloisonnement tient
	// pendant tout le dépistage.
	Indexes []string `json:"indexes,omitempty"`
	// Archives sont des ZIP de copies reçues d'un collègue. Elles entrent dans
	// le corpus par le même chemin que les dépôts, et leur chemin est retenu
	// dans le rapport plutôt que leur contenu : un rapport dit alors d'où les
	// copies venaient sans porter une ligne de leur code.
	Archives []string `json:"archives,omitempty"`
	// Baseline est le gabarit distribué : dépôt modèle, dossier de départ
	// déposé. Ses empreintes sont écartées d'office, puisque toutes les copies
	// les portent sans que personne n'ait rien copié.
	Baseline []corpus.Target `json:"baseline,omitempty"`

	Inspection inspect.Settings `json:"inspection"`
	// Rules porte ce que l'organisation déclare : les profils d'inspection de
	// l'équipe, et ses bornes préférées. Elles sont recopiées dans la demande
	// plutôt que relues à l'exécution — un rapport doit pouvoir dire sous
	// quelles règles il a été produit, même si elles ont changé depuis.
	Rules rules.Rules `json:"rules,omitzero"`
	// Kgram et Window remplacent les bornes du langage ; zéro garde les
	// siennes.
	Kgram  int `json:"kgram,omitempty"`
	Window int `json:"window,omitempty"`
	// Noise, MinSimilarity et MaxMatches règlent la comparaison.
	Noise         float64 `json:"noise,omitempty"`
	MinSimilarity float64 `json:"min_similarity,omitempty"`
	MaxMatches    int     `json:"max_matches,omitempty"`
	Jobs          int     `json:"jobs,omitempty"`
}

// Validate refuse une demande qui ne peut pas aboutir, avant tout
// téléchargement. Refuser après dix minutes d'attente serait une faute.
func (r Request) Validate() error {
	// Les copies reçues comptent : comparer deux dépôts d'ici, ou un dépôt d'ici
	// avec une archive d'ailleurs, sont deux demandes également valables.
	if len(r.Targets)+len(r.Archives)+len(r.Indexes) < 2 {
		return valid.Errorf(
			"Détection de plagiat : il faut au moins deux copies à comparer "+
				"(%s, %s et %s %s).",
			valid.Plural(len(r.Targets), "dépôt"), valid.Plural(len(r.Archives), "archive"),
			valid.Plural(len(r.Indexes), "index", "index"),
			valid.Agree(len(r.Targets)+len(r.Archives)+len(r.Indexes), "fourni"))
	}
	return r.ValidateSettings()
}

// ValidateSettings vérifie tout sauf le nombre de copies.
//
// Un envoi ne compare rien : envoyer une seule copie à un collègue qui la
// demande est légitime, et la règle des deux copies n'y a pas sa place. Les
// réglages, eux, doivent tenir dans les deux cas.
func (r Request) ValidateSettings() error {
	if err := r.ValidateArchives(); err != nil {
		return err
	}
	if r.Noise > 1 {
		return valid.Errorf(
			"Empreintes communes : la part doit être comprise entre 0 et 1 (%.2f fournie).",
			r.Noise)
	}
	if r.MinSimilarity < 0 || r.MinSimilarity > 1 {
		return valid.Errorf(
			"Similarité minimale : elle doit être comprise entre 0 et 1 (%.2f fournie).",
			r.MinSimilarity)
	}
	if r.Kgram < 0 || r.Window < 0 {
		return valid.Errorf("Bornes de winnowing : elles ne peuvent pas être négatives.")
	}
	if _, err := inspect.New(r.Inspection, r.Rules.Profiles); err != nil {
		return err
	}
	if _, err := r.Rules.Validate(); err != nil {
		return err
	}
	return nil
}

// normalized comble ce que la demande laisse en blanc par ce que
// l'organisation préfère.
//
// Les bornes vivent dans la demande une fois comblées, et non dans les règles :
// un rapport dit alors avec quelles bornes il a été produit, sans qu'il faille
// retrouver l'état du registre de ce jour-là.
func (r Request) normalized() Request {
	if r.Kgram == 0 {
		r.Kgram = r.Rules.Defaults.Kgram
	}
	if r.Window == 0 {
		r.Window = r.Rules.Defaults.Window
	}
	if r.Noise == 0 {
		r.Noise = r.Rules.Defaults.Noise
	}
	return r
}

// Report est ce qu'une analyse rend, tel qu'il s'écrit et se relit.
type Report struct {
	Version   int    `json:"version"`
	Tool      string `json:"tool,omitempty"`
	CreatedAt string `json:"created_at"`
	// Disclaimer voyage avec le rapport. Un fichier transmis à un collègue, ou
	// relu dans six mois, doit porter son propre avertissement.
	Disclaimer string  `json:"disclaimer"`
	Request    Request `json:"request"`
	// Profile nomme le profil d'inspection réellement appliqué.
	Profile inspect.Profile `json:"profile"`
	// Index est le corpus d'empreintes. Il est gardé pour que rouvrir le
	// rapport, déplacer le seuil ou ajouter les copies d'un collègue ne
	// demande pas de tout retélécharger.
	Index     similarity.Corpus  `json:"index"`
	Result    similarity.Report  `json:"result"`
	Problems  []corpus.Problem   `json:"problems,omitempty"`
	Inspected []corpus.Inspected `json:"inspected,omitempty"`
	// Reused compte les copies reprises d'une analyse antérieure au lieu d'être
	// retéléchargées. Le dire n'est pas cosmétique : une copie reprise a été
	// empreintée sur un état plus ancien, et l'enseignant doit pouvoir le
	// savoir si un dépôt lui paraît ne pas correspondre.
	Reused int `json:"reused,omitempty"`
	// Received compte les copies venues d'ailleurs que de GitHub — une archive
	// reçue, un index publié. Elles n'ont ni dépôt ni commit, et leur identité
	// n'est connue que de qui les a publiées.
	Received int `json:"received,omitempty"`
	// Screened compte celles qui viennent d'un index publié : on en a mesuré
	// les ressemblances sans jamais en lire une ligne.
	Screened int `json:"screened,omitempty"`
}

// Commit rend le commit archivé d'une copie, pour la retélécharger à
// l'identique quand on ouvre une paire.
func (r *Report) Commit(id string) string {
	for _, inspected := range r.Inspected {
		if inspected.ID == id {
			return inspected.Commit
		}
	}
	return ""
}

// Target retrouve la cible d'une copie.
func (r *Report) Target(id string) (corpus.Target, bool) {
	for _, target := range r.Request.Targets {
		if target.ID == id {
			return target, true
		}
	}
	return corpus.Target{}, false
}

// Screening dit, pour chaque copie venue d'un index publié, de quel travail
// elle vient — et par là, à qui la demander.
//
// C'est la seule distinction qui compte à la lecture d'un rapport mêlé : une
// copie qu'on a lue s'ouvre, une copie qu'on a seulement mesurée ne s'ouvre pas.
// Laisser croire le contraire ferait cliquer dans le vide.
func (r *Report) Screening() map[string]string {
	depistees := make(map[string]string, 8)
	for _, inspected := range r.Inspected {
		if inspected.Index != "" {
			depistees[inspected.ID] = inspected.Index
		}
	}
	return depistees
}

// Analyzed compte les copies réellement comparées.
func (r *Report) Analyzed() int { return len(r.Index.Works) }

// Barren dit qu'aucune comparaison n'a eu lieu, et pourquoi.
//
// « Aucune paire au-dessus du seuil » et « aucune copie n'a pu être lue » se
// ressemblent beaucoup à l'écran et ne veulent pas du tout dire la même chose.
// La première rassure ; la seconde n'apprend rien, et la faire passer pour la
// première serait le pire service à rendre — on croirait avoir regardé.
//
// La règle est ici plutôt que dans chaque interface : le navigateur, le
// terminal et la ligne de commande doivent s'arrêter au même endroit.
func (r *Report) Barren() error {
	if r.Analyzed() >= 2 {
		return nil
	}
	detail := "aucun dépôt n'a pu être analysé"
	if r.Analyzed() == 1 {
		detail = "une seule copie a pu être analysée, et il en faut deux"
	}
	if motif, count := r.MainProblem(); motif != "" {
		detail += fmt.Sprintf(" (%d fois : %s)", count, motif)
	}
	return valid.Errorf("Détection de plagiat : %s. Rien n'a été comparé.", detail)
}

// MainProblem rend le motif qui a le plus écarté de dépôts.
func (r *Report) MainProblem() (string, int) {
	counts := map[string]int{}
	for _, problem := range r.Problems {
		counts[problem.Reason]++
	}
	best, most := "", 0
	for reason, count := range counts {
		if count > most || (count == most && reason < best) {
			best, most = reason, count
		}
	}
	return best, most
}

// Run mène l'analyse.
func Run(client corpus.Client, request Request,
	progress func(done, total int, id string)) (*Report, error) {
	return RunWith(client, request, nil, progress)
}

// RunWith mène l'analyse en reprenant ce que d'anciens rapports ont déjà
// empreinté.
func RunWith(client corpus.Client, request Request, prior []*Report,
	progress func(done, total int, id string)) (*Report, error) {
	return RunFrom(Sources{Client: client, Prior: prior}, request, progress)
}

// RunFrom mène l'analyse à partir de toutes ses provenances.
//
// Quatre chemins y mènent, et ils se rejoignent tous au même endroit : les
// dépôts de GitHub, les index d'anciens rapports — une session passée ne change
// plus, la retélécharger ne calculerait que des empreintes identiques —, les
// index publiés par des collègues, et les archives reçues. Ce qui suit ne fait
// plus la différence, et c'est ce qui garantit que les mesures se comparent.
func RunFrom(sources Sources, request Request,
	progress func(done, total int, id string)) (*Report, error) {

	request = request.normalized()
	if err := request.Validate(); err != nil {
		return nil, err
	}
	inspector, err := inspect.New(request.Inspection, request.Rules.Profiles)
	if err != nil {
		return nil, err
	}

	options := corpus.Options{
		Inspector: inspector, Kgram: request.Kgram,
		Window: request.Window, Jobs: request.Jobs,
	}
	reuse := Reusable(request, sources.Prior)
	fresh, kept := reuse.Split(request.Targets)
	built := corpus.Build(sources.Client, fresh, options, progress)

	// Les copies reçues d'un collègue entrent ici, après celles d'ici : leurs
	// identifiants sont des jetons opaques, et l'on veut savoir si l'un d'eux
	// écraserait un de nos dépôts.
	pris := map[string]bool{}
	for _, work := range built.Corpus.Works {
		pris[work.ID] = true
	}
	// Les index publiés par les collègues entrent avant les archives : ce sont
	// des empreintes seules, et ils ne coûtent qu'une lecture.
	publies, vuesPubliees, souciesPubliees := loadIndexes(sources, request, pris)
	for _, work := range publies {
		built.Corpus.Add(work)
	}
	built.Inspected = append(built.Inspected, vuesPubliees...)
	built.Problems = append(built.Problems, souciesPubliees...)

	recues, vues, soucis := loadArchives(request, options, pris)
	for _, work := range recues {
		built.Corpus.Add(work)
	}
	built.Inspected = append(built.Inspected, vues...)
	built.Problems = append(built.Problems, soucis...)

	// Ce qui est repris rejoint ce qui vient d'être lu. L'ordre n'a pas
	// d'importance pour la mesure, mais il en a pour la lecture : le corpus est
	// rangé pour que deux analyses du même travail se comparent ligne à ligne.
	repris, dejaVus := reuse.Take(kept)
	for _, work := range repris {
		built.Corpus.Add(work)
	}
	built.Inspected = append(built.Inspected, dejaVus...)
	built.Corpus.Sort()

	report := &Report{
		Version: Version, CreatedAt: time.Now().Format(time.RFC3339),
		Disclaimer: Disclaimer, Request: request, Profile: inspector.Profile(),
		Index: built.Corpus, Problems: built.Problems, Inspected: built.Inspected,
		Reused: len(repris), Received: len(recues) + len(publies),
		Screened: len(publies),
	}

	baseline, ordinaire, issues := gabarit(sources.Client, request.Baseline, options)
	report.Problems = append(report.Problems, issues...)

	report.Result = similarity.Compare(built.Corpus, similarity.Options{
		Noise: request.Noise, MinSimilarity: request.MinSimilarity,
		MaxMatches: request.MaxMatches, Baseline: baseline, Ordinary: ordinaire,
	})
	return report, nil
}

// gabarit empreinte le modèle distribué, et relève ce qu'il dit de lui-même.
//
// Deux choses en sortent, pour deux usages. Les empreintes sont retirées de la
// mesure ; les commentaires et les chaînes du gabarit sont retirés des signaux,
// où un en-tête imposé ou un message d'erreur fourni se retrouverait sinon
// « partagé » par tout le groupe.
//
// Un gabarit qu'on n'arrive pas à lire n'arrête pas l'analyse : elle se fait
// sans lui, le garde-fou statistique reprenant le travail. Mais cela se dit —
// sans le retrait du gabarit, les scores sont tous gonflés de la même
// quantité, et l'enseignant doit savoir qu'il les lit ainsi.
func gabarit(client corpus.Client, targets []corpus.Target, options corpus.Options) (
	map[uint64]struct{}, similarity.Signals, []corpus.Problem) {

	if len(targets) == 0 {
		return nil, similarity.Signals{}, nil
	}
	built := corpus.Build(client, targets, options, nil)
	if len(built.Corpus.Works) == 0 {
		return nil, similarity.Signals{}, built.Problems
	}
	return similarity.Baseline(built.Corpus.Works...),
		similarity.Ordinary(built.Corpus.Works...), built.Problems
}
