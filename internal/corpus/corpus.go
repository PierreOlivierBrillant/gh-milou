// Package corpus rassemble ce qui sera comparé : quels dépôts, quels fichiers
// dedans, et — tout aussi important — lesquels n'ont pas pu l'être et pourquoi.
//
// Un rapport de plagiat qui passe des dépôts sous silence est pire qu'inutile :
// il laisse croire que ce qui n'y figure pas a été regardé. Un dépôt vide, un
// accès refusé par le cloisonnement, un profil d'inspection qui ne trouve
// aucun fichier — chacun de ces cas produit ici un motif nommé, rapporté à
// côté des résultats et jamais en dessous.
package corpus

import (
	"sort"
	"strings"
	"sync"

	"github.com/PierreOlivierBrillant/gh-milou/internal/ghapi"
	"github.com/PierreOlivierBrillant/gh-milou/internal/inspect"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/signature"
	"github.com/PierreOlivierBrillant/gh-milou/internal/similarity"
	"github.com/PierreOlivierBrillant/gh-milou/internal/tokens"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Motifs d'un dépôt non analysé, tels que les trois interfaces les affichent.
const (
	NoCommit    = "dépôt sans commit"
	Denied      = "accès refusé"
	Unreachable = "archive inaccessible"
	Broken      = "archive illisible"
	NothingKept = "aucun fichier retenu par le profil"
	TooShort    = "trop peu de code pour être comparé"
)

// DefaultJobs est le nombre de dépôts récupérés de front. Quatre est ce que le
// reste de l'outil utilise déjà, et ce que l'API de GitHub supporte sans
// rechigner.
const DefaultJobs = 4

// Target est un dépôt à analyser.
type Target struct {
	// ID désigne la copie dans le rapport : le nom du dépôt, ou un jeton
	// opaque quand elle vient d'ailleurs.
	ID    string
	Label string
	// Origin étiquette la provenance — le groupe, la session, l'enseignant.
	Origin string
	Owner  string
	Repo   string
	// Ref épingle la remise. Vide prend la branche par défaut, ce qui expose à
	// analyser un dépôt qui a bougé depuis : le commit réellement archivé est
	// donc noté au retour.
	Ref string
	// Person est l'identité derrière la copie. Elle ne sert pas à l'analyse :
	// elle sert à l'effacer, le jour où l'on anonymise ces copies pour les
	// transmettre. Vide, seul le nom du dépôt sera cherché.
	Person roster.Person
	// Token impose le jeton sous lequel cette copie sera anonymisée. Il sert
	// quand elle a déjà voyagé sous un jeton — celui d'un index publié — et
	// qu'elle doit repartir sous le même : sans quoi celui qui la reçoit ne
	// pourrait pas reconnaître la copie qu'il avait mesurée.
	Token string
	// HandedIn date la remise, quand on l'a déjà relevée. Elle ne sert pas non
	// plus à l'analyse : elle accompagne la table de correspondance d'un envoi
	// anonymisé, où savoir qui a remis en premier compte.
	HandedIn string
	// PushedAt est la date du dernier envoi, telle que l'inventaire la donne.
	// Elle ne sert pas à l'analyse : elle sert à savoir qu'une copie n'a pas
	// bougé depuis qu'on l'a empreintée, et qu'il est donc inutile de la
	// retélécharger.
	PushedAt string
}

// FullName rend « organisation/depot ».
func (t Target) FullName() string { return t.Owner + "/" + t.Repo }

// Problem dit qu'un dépôt n'a pas pu être analysé, et pourquoi.
type Problem struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Reason string `json:"reason"`
	// Detail porte ce que le motif ne dit pas : le message de GitHub, le
	// nombre de fichiers vus, la racine devinée.
	Detail string `json:"detail,omitempty"`
	// Skipped n'est rempli que lorsque rien n'a été retenu. C'est le seul cas
	// où il faut voir fichier par fichier : partout ailleurs, les décomptes
	// suffisent, et la liste entière noierait le rapport.
	Skipped []inspect.Skipped `json:"skipped,omitempty"`
}

// Inspected résume ce qu'une copie a donné.
type Inspected struct {
	ID   string `json:"id"`
	Repo string `json:"repo"`
	// Root est la racine du projet retenue. Elle est rapportée parce qu'une
	// racine mal devinée explique à elle seule une copie qui paraît vide.
	Root    string `json:"root"`
	Commit  string `json:"commit,omitempty"`
	Kept    int    `json:"kept"`
	Skipped int    `json:"skipped"`
	// Signed dit que la copie porte une marque invisible. Son absence ne prouve
	// rien — un formateur l'efface sans le savoir —, mais elle se rapporte :
	// une copie non signée est une copie dont ce signal ne dira jamais rien.
	Signed bool `json:"signed,omitempty"`
	// Short compte les fichiers retenus mais trop courts pour former un
	// k-gramme. Ils restent dans l'index — ils ont été retenus, ils doivent
	// rester visibles —, mais ils ne peuvent s'apparier à rien.
	Short int `json:"short"`
	// Index nomme le travail dont l'index publié a fourni cette copie. Il est
	// vide pour tout ce qui vient d'un dépôt : c'est lui qui distingue une
	// copie qu'on a lue d'une copie qu'on a seulement mesurée, et donc celles
	// qu'il faudra demander pour en voir une ligne.
	Index   string         `json:"index,omitempty"`
	Bytes   int            `json:"bytes"`
	Tokens  int            `json:"tokens"`
	Prints  int            `json:"prints"`
	Reasons map[string]int `json:"reasons,omitempty"`
}

// Result est ce qu'une constitution de corpus rend.
type Result struct {
	Corpus    similarity.Corpus
	Problems  []Problem
	Inspected []Inspected
}

// Totals additionne ce que le corpus pèse.
func (r Result) Totals() (files, bytes, tokens, prints int) {
	for _, entry := range r.Inspected {
		files += entry.Kept
		bytes += entry.Bytes
		tokens += entry.Tokens
		prints += entry.Prints
	}
	return files, bytes, tokens, prints
}

// Client est ce que la constitution du corpus attend de GitHub. L'interface est
// réduite à une méthode pour que les épreuves n'aient pas à monter un serveur.
type Client interface {
	Archive(owner, repo, ref string) ([]byte, error)
}

// Options règle la constitution d'un corpus.
type Options struct {
	Inspector *inspect.Inspector
	// Kgram et Window remplacent les bornes du langage. Zéro garde les siennes.
	Kgram, Window int
	Jobs          int
}

// Build récupère les dépôts et en tire un index comparable.
//
// Le résultat ne porte aucun contenu : des empreintes, des décomptes, des
// chemins. Quand une paire est ouverte, les deux dépôts concernés sont
// retéléchargés — deux archives plutôt que trois cents gardées en mémoire pour
// le cas où l'on en regarderait deux.
func Build(client Client, targets []Target, options Options,
	progress func(done, total int, id string)) Result {

	jobs := options.Jobs
	if jobs < 1 {
		jobs = DefaultJobs
	}
	if jobs > len(targets) {
		jobs = len(targets)
	}

	type outcome struct {
		work      similarity.Work
		inspected Inspected
		problem   *Problem
		analyzed  bool
	}
	outcomes := make([]outcome, len(targets))

	var wait sync.WaitGroup
	var counter sync.Mutex
	done := 0
	queue := make(chan int)

	for worker := 0; worker < jobs; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for position := range queue {
				target := targets[position]
				work, inspected, problem := Analyze(client, target, options)
				outcomes[position] = outcome{
					work: work, inspected: inspected, problem: problem,
					analyzed: problem == nil,
				}
				if progress != nil {
					counter.Lock()
					done++
					current := done
					counter.Unlock()
					progress(current, len(targets), target.ID)
				}
			}
		}()
	}
	for position := range targets {
		queue <- position
	}
	close(queue)
	wait.Wait()

	result := Result{}
	for _, outcome := range outcomes {
		if outcome.problem != nil {
			result.Problems = append(result.Problems, *outcome.problem)
			continue
		}
		result.Corpus.Works = append(result.Corpus.Works, outcome.work)
		result.Inspected = append(result.Inspected, outcome.inspected)
	}
	return result
}

// Analyze récupère un dépôt et en tire une copie d'index.
func Analyze(client Client, target Target, options Options) (
	similarity.Work, Inspected, *Problem) {

	archive, err := client.Archive(target.Owner, target.Repo, target.Ref)
	if err != nil {
		return similarity.Work{}, Inspected{}, FetchProblem(target, err)
	}
	sources, prefix, err := Untar(archive)
	if err != nil {
		return similarity.Work{}, Inspected{},
			&Problem{ID: target.ID, Repo: target.FullName(), Reason: Broken,
				Detail: err.Error()}
	}

	loose := Loose{
		ID: target.ID, Label: target.Label, Origin: target.Origin,
		Repo: target.FullName(), Commit: Commit(prefix), Sources: sources,
	}
	return Fingerprint(loose, options)
}

// Loose est une copie déjà en main : sortie d'une archive de GitHub, ou reçue
// d'un collègue dans un ZIP. Ce qui suit ne fait plus la différence.
type Loose struct {
	ID     string
	Label  string
	Origin string
	// Repo et Commit ne valent que pour une copie venue de GitHub. Une copie
	// reçue n'en a pas, et son rapport le dira plutôt que d'inventer.
	Repo    string
	Commit  string
	Sources []inspect.Source
}

// Fingerprint inspecte une copie et en tire ses empreintes.
//
// C'est le point où les deux provenances se rejoignent. Une copie téléchargée
// de GitHub et une copie reçue d'un collègue doivent être traitées exactement
// de la même façon — mêmes exclusions, mêmes bornes, mêmes motifs d'écartement
// — sinon leurs mesures ne se compareraient pas entre elles.
func Fingerprint(loose Loose, options Options) (similarity.Work, Inspected, *Problem) {
	selection := options.Inspector.Select(loose.Sources)
	nom := loose.Repo
	if nom == "" {
		nom = loose.ID
	}
	inspected := Inspected{
		ID: loose.ID, Repo: nom, Root: selection.Root,
		Commit: loose.Commit, Kept: len(selection.Kept),
		Skipped: len(selection.Skipped), Bytes: selection.Bytes(),
		Reasons: selection.Reasons,
	}
	if len(selection.Kept) == 0 {
		return similarity.Work{}, inspected, &Problem{
			ID: loose.ID, Repo: nom, Reason: NothingKept,
			Detail:  detailOf(selection),
			Skipped: selection.Skipped,
		}
	}

	work := similarity.Work{ID: loose.ID, Label: loose.Label, Origin: loose.Origin}
	for _, kept := range selection.Kept {
		// La marque invisible, s'il y en a une. Elle est cherchée dans tout ce
		// qui a été retenu, et non dans le seul README : un étudiant qui
		// déplace un fichier ne la fait pas disparaître, et la chercher partout
		// ne coûte qu'un balayage de lignes vides.
		if work.Extras.Signature == "" {
			if token, signee := signature.First(kept.Content); signee {
				work.Extras.Signature = signature.Text(token)
				inspected.Signed = true
			}
		}
		analysis := tokens.Lex(kept.Language, kept.Content)
		file, short := similarity.Analyze(kept.Path, analysis, options.Kgram, options.Window)
		inspected.Tokens += file.Tokens
		// Un fichier trop court n'est pas une anomalie : c'est une constante,
		// une interface, un fichier de barils. Il entre quand même dans
		// l'index, sans empreinte : il a été retenu par le profil, et le faire
		// disparaître du rapport laisserait croire qu'on l'a comparé.
		if short != nil {
			inspected.Short++
		}
		inspected.Prints += len(file.Prints)
		work.Files = append(work.Files, file)
		work.Extras.Comments = append(work.Extras.Comments, longComments(analysis)...)
		work.Extras.Literals = append(work.Extras.Literals,
			longLiterals(kept.Content, analysis)...)
	}

	if inspected.Prints == 0 {
		return similarity.Work{}, inspected, &Problem{
			ID: loose.ID, Repo: nom, Reason: TooShort,
			Detail: valid.Plural(len(selection.Kept), "fichier retenu", "fichiers retenus") +
				", tous trop courts pour former un k-gramme",
		}
	}
	return work, inspected, nil
}

// FetchProblem traduit un échec de GitHub en motif compréhensible.
//
// Le statut compte : un 403 vient du cloisonnement — un collègue a un groupe
// qu'on ne voit pas —, un 404 sur une archive vient d'un dépôt sans commit,
// puisque l'inventaire ne montre que les dépôts qu'on peut lire.
func FetchProblem(target Target, err error) *Problem {
	problem := &Problem{ID: target.ID, Repo: target.FullName(), Detail: err.Error()}
	switch ghapi.StatusOf(err) {
	case 403, 401:
		problem.Reason = Denied
	case 404:
		problem.Reason = NoCommit
	default:
		problem.Reason = Unreachable
	}
	return problem
}

// detailOf dit pourquoi une inspection n'a rien retenu, par le motif qui a le
// plus écarté. « Aucun fichier retenu » sans autre mot n'aide personne.
func detailOf(selection inspect.Selection) string {
	detail := valid.Plural(len(selection.Skipped), "fichier vu", "fichiers vus")
	if motifs := selection.Summary(); len(motifs) > 0 {
		detail += ", surtout : " + motifs[0]
	}
	if selection.Root != "" {
		detail += " (racine retenue : " + selection.Root + ")"
	}
	return detail
}

// longLiterals relève les chaînes de caractères assez longues pour être
// parlantes.
//
// Le flux comparé les a toutes réduites à « STR » — c'est ce qui fait que deux
// copies ne différant que par une constante restent appariées —, mais les
// jetons gardent leurs bornes en octets : le texte d'origine se relit dans le
// fichier, sans que les analyseurs lexicaux aient à le conserver.
//
// Les délimiteurs sont retirés et les espaces resserrés, comme pour les
// commentaires : deux chaînes qui ne diffèrent que par leurs guillemets sont la
// même phrase.
func longLiterals(content []byte, analysis tokens.Result) []string {
	kept := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, token := range analysis.Tokens {
		if token.Kind != tokens.Text || token.Start < 0 || token.End > len(content) ||
			token.Start >= token.End {
			continue
		}
		texte := reduire(string(content[token.Start:token.End]))
		if len([]rune(texte)) < similarity.MinSharedLiteral || seen[texte] {
			continue
		}
		seen[texte] = true
		kept = append(kept, texte)
	}
	sort.Strings(kept)
	return kept
}

// reduire retire les délimiteurs d'une chaîne et resserre ses espaces.
func reduire(texte string) string {
	texte = strings.TrimFunc(texte, func(r rune) bool {
		return r == '"' || r == '\'' || r == '`' || r == '@' || r == '$'
	})
	return strings.Join(strings.Fields(texte), " ")
}

// longComments ne garde que les commentaires assez longs pour qu'un partage
// veuille dire quelque chose. Les garder tous ferait peser un corpus entier de
// « à faire » et de « constructeur ».
func longComments(analysis tokens.Result) []string {
	kept := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, note := range analysis.Comments {
		if len([]rune(note.Text)) < similarity.MinSharedComment || seen[note.Text] {
			continue
		}
		seen[note.Text] = true
		kept = append(kept, note.Text)
	}
	sort.Strings(kept)
	return kept
}
