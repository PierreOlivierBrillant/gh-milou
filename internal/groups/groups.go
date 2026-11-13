// Package groups regroupe les dépôts d'une organisation par préfixe commun
// et interprète les sélections saisies au clavier.
package groups

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Separator sépare les segments d'un nom de dépôt.
const Separator = "-"

// Mots qui désignent la totalité d'une sélection.
var allKeywords = map[string]bool{
	"tous": true, "toutes": true, "tout": true, "all": true, "*": true,
}

var rangeRe = regexp.MustCompile(`^(\d+)\s*-\s*(\d+)$`)

// RepoInfo retient les seuls champs utiles d'un dépôt : le reste de la réponse
// de l'API alourdirait le cache pour rien.
type RepoInfo struct {
	Name     string `json:"name"`
	Private  bool   `json:"private"`
	HTMLURL  string `json:"html_url"`
	PushedAt string `json:"pushed_at"`
}

// Service dit qu'un dépôt n'appartient à aucun groupe : c'est un dépôt de
// service de l'organisation. GitHub en réserve un — « .github », qui porte les
// gabarits communs — et l'outil y rangera le sien.
//
// Le point de tête suffit à les reconnaître, et il ne peut désigner qu'eux : la
// slugification ne le produit jamais, et un nom de la nomenclature qui
// commencerait par lui aurait un premier niveau vide.
func Service(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), ".")
}

// Ordinary écarte d'un inventaire les dépôts de service. C'est fait une fois,
// là où l'inventaire est chargé, pour que ni la détection des groupes, ni les
// listes, ni les décomptes n'aient à s'en préoccuper.
func Ordinary(repos []RepoInfo) []RepoInfo {
	gardes := make([]RepoInfo, 0, len(repos))
	for _, repo := range repos {
		if !Service(repo.Name) {
			gardes = append(gardes, repo)
		}
	}
	return gardes
}

// Une écriture ne périme pas forcément l'inventaire. Renommer ou supprimer un
// dépôt est un changement qu'on connaît exactement : le répercuter sur
// l'inventaire déjà en main évite de le relire en entier. À l'échelle d'un
// département — plusieurs milliers de dépôts, des dizaines de pages —, c'est la
// différence entre un geste instantané et une attente à chaque fois.
//
// Ce que devient un inventaire se décide ici, et non dans les interfaces : le
// terminal et le navigateur font le même geste et doivent en tirer le même
// inventaire. Chacun garde en revanche sa façon de retenir sa copie.
//
// Une création, elle, oblige à relire. Le dépôt neuf n'est pas dans
// l'inventaire, et sa date de dernier envoi ne s'invente pas : les fichiers de
// départ y sont déposés après sa création, si bien que ce que GitHub a répondu
// à la création est déjà dépassé. Une date inventée fausserait la colonne
// « dernier envoi » et le filtre des muets.

// Renamed est un dépôt renommé : son ancien nom, et ce qu'il est devenu.
type Renamed struct {
	Before string
	After  RepoInfo
}

// WithRenamed suit dans un inventaire les dépôts qu'on vient de renommer. Un
// renommage à moitié fait n'est pas un problème : seuls ceux qui ont abouti
// sont dans la liste.
func WithRenamed(repos []RepoInfo, done []Renamed) []RepoInfo {
	if len(repos) == 0 || len(done) == 0 {
		return repos
	}
	suivis := make(map[string]RepoInfo, len(done))
	for _, item := range done {
		suivis[strings.ToLower(item.Before)] = item.After
	}
	suivi := make([]RepoInfo, 0, len(repos))
	for _, repo := range repos {
		if apres, change := suivis[strings.ToLower(repo.Name)]; change {
			// La date de dernier envoi ne bouge pas : renommer n'est pas pousser.
			repo.Name, repo.HTMLURL, repo.Private = apres.Name, apres.HTMLURL, apres.Private
		}
		suivi = append(suivi, repo)
	}
	return suivi
}

// WithoutRepo retire d'un inventaire un dépôt qu'on vient de supprimer.
func WithoutRepo(repos []RepoInfo, name string) []RepoInfo {
	restants := make([]RepoInfo, 0, len(repos))
	for _, repo := range repos {
		if !strings.EqualFold(repo.Name, name) {
			restants = append(restants, repo)
		}
	}
	return restants
}

// Repo est un dépôt appartenant à un groupe.
type Repo struct {
	Name    string
	Suffix  string // ce qui suit le préfixe : compte GitHub ou nom slugifié
	Private bool
	URL     string
	// PushedAt est le dernier envoi, à l'heure de la machine — « 2026-08-21
	// 14:32 ». Une remise du soir se joue à la minute : la journée seule ne
	// dirait pas si elle précède l'échéance.
	PushedAt string
}

// Visibility décrit la visibilité en français.
func (r Repo) Visibility() string {
	if r.Private {
		return "privé"
	}
	return "public"
}

// Group rassemble les dépôts partageant un même préfixe.
type Group struct {
	Prefix string
	Repos  []Repo
}

// Len renvoie le nombre de dépôts du groupe.
func (g Group) Len() int { return len(g.Repos) }

// Find retrouve un dépôt par son nom complet ou son suffixe, sans tenir compte de la casse.
func (g Group) Find(name string) (Repo, int, bool) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	for index, repo := range g.Repos {
		if strings.ToLower(repo.Name) == wanted || strings.ToLower(repo.Suffix) == wanted {
			return repo, index, true
		}
	}
	return Repo{}, -1, false
}

// Detected est un groupe deviné à partir des noms de dépôts.
type Detected struct {
	Prefix string `json:"prefix"`
	Count  int    `json:"count"`
}

// segmented découpe les noms en segments, en minuscules. Un nom sans séparateur
// ne peut porter aucun préfixe : il est écarté.
func segmented(names []string) [][]string {
	members := make([][]string, 0, len(names))
	for _, name := range names {
		segments := strings.Split(strings.ToLower(name), Separator)
		if len(segments) < 2 {
			continue
		}
		members = append(members, segments)
	}
	return members
}

// branches répartit les dépôts selon le segment qui suit le préfixe de longueur
// « depth ». Un dépôt dont il ne reste qu'un segment — son suffixe — ne peut pas
// descendre plus bas : il est compté à part.
func branches(members [][]string, depth int) (map[string][][]string, int) {
	byNext := make(map[string][][]string)
	terminal := 0
	for _, segments := range members {
		if len(segments) <= depth+1 {
			terminal++
			continue
		}
		next := segments[depth]
		byNext[next] = append(byNext[next], segments)
	}
	return byNext, terminal
}

// splits dit si le préfixe se subdivise en travaux distincts : au moins deux
// sous-préfixes rassemblent chacun un groupe entier, et les dépôts qui
// s'arrêtent au préfixe sont trop peu nombreux pour en former un eux-mêmes.
// C'est le cas de « a26 » quand l'organisation porte « a26-4w6-tp1-… » et
// « a26-5n6-travailsession-… » : deux travaux que fondre dans « a26 »
// n'aiderait personne. À l'inverse, un groupe dont les suffixes sont des
// comptes GitHub garde toujours des noms d'un seul segment — « tp1-jlpicard »
// à côté de « tp1-emilie-cote » — et reste donc entier.
func splits(byNext map[string][][]string, terminal, minimum int) bool {
	if terminal >= minimum {
		return false
	}
	subgroups := 0
	for _, sub := range byNext {
		if len(sub) >= minimum {
			subgroups++
		}
	}
	return subgroups >= 2
}

// collect retient le groupe formé par les « depth » premiers segments, ou
// descend dans ses sous-groupes s'il se subdivise.
func collect(members [][]string, depth, minimum int, found *[]Detected) {
	// Le préfixe est d'abord étendu à ce que partagent réellement ses membres :
	// « projet » redevient « projet-final » si tous les dépôts le portent.
	for {
		byNext, terminal := branches(members, depth)
		if terminal > 0 || len(byNext) != 1 {
			break
		}
		depth++
	}
	byNext, terminal := branches(members, depth)
	if splits(byNext, terminal, minimum) {
		for _, sub := range byNext {
			if len(sub) >= minimum {
				collect(sub, depth+1, minimum, found)
			}
		}
		return
	}
	if len(members) >= minimum {
		*found = append(*found, Detected{
			Prefix: strings.Join(members[0][:depth], Separator),
			Count:  len(members),
		})
	}
}

// Detect devine les groupes présents et les renvoie du plus grand au plus petit.
// Le préfixe retenu est le plus général qui rassemble au moins « minimum »
// dépôts sans se subdiviser en plusieurs groupes de cette taille.
func Detect(names []string, minimum int) []Detected {
	if minimum < 1 {
		minimum = 2
	}
	// Le premier segment est toujours un point de départ distinct : rien ne
	// rassemble « tp1-… » et « tp2-… ». Les branches trop petites tombent ici.
	roots, _ := branches(segmented(names), 0)
	detected := make([]Detected, 0, len(roots))
	for _, members := range roots {
		if len(members) < minimum {
			continue
		}
		collect(members, 1, minimum, &detected)
	}
	sort.Slice(detected, func(i, j int) bool {
		if detected[i].Count != detected[j].Count {
			return detected[i].Count > detected[j].Count
		}
		return detected[i].Prefix < detected[j].Prefix
	})
	return detected
}

// Separators sont les caractères qui peuvent suivre un préfixe : le tiret des
// noms qu'aucune convention n'organise, et le point de la nomenclature à cinq
// niveaux. Un préfixe se saisit sans dire lequel des deux le termine.
const Separators = Separator + "."

// Build retient les dépôts du préfixe donné, à partir des données de l'API.
func Build(prefix string, repos []RepoInfo) Group {
	wanted := strings.TrimRight(strings.ToLower(strings.TrimSpace(prefix)), Separators)
	group := Group{Prefix: wanted}
	if wanted == "" {
		return group
	}
	for _, raw := range repos {
		if !suit(strings.ToLower(raw.Name), wanted) {
			continue
		}
		group.Repos = append(group.Repos, Repo{
			Name: raw.Name,
			// Les séparateurs en trop ne sont à personne : « TP3-KickMyB--alice »
			// désigne « alice », pas « -alice ». Un compte GitHub ne commence
			// pas par un tiret, un nom slugifié non plus.
			Suffix:   strings.TrimLeft(raw.Name[len(wanted)+1:], Separators),
			Private:  raw.Private,
			URL:      raw.HTMLURL,
			PushedAt: valid.Moment(raw.PushedAt),
		})
	}
	sort.Slice(group.Repos, func(i, j int) bool {
		return strings.ToLower(group.Repos[i].Name) < strings.ToLower(group.Repos[j].Name)
	})
	return group
}

// Dated rend le groupe dont chaque dépôt prend le dernier envoi que
// l'inventaire donné lui prête — celui qu'« Activity » a corrigé, par exemple.
// Un dépôt que l'inventaire ignore garde le sien.
func (g Group) Dated(repos []RepoInfo) Group {
	envois := make(map[string]string, len(repos))
	for _, repo := range repos {
		envois[strings.ToLower(repo.Name)] = repo.PushedAt
	}
	date := Group{Prefix: g.Prefix, Repos: make([]Repo, len(g.Repos))}
	for index, repo := range g.Repos {
		if envoi, connu := envois[strings.ToLower(repo.Name)]; connu {
			repo.PushedAt = valid.Moment(envoi)
		}
		date.Repos[index] = repo
	}
	return date
}

// suit dit si un nom commence par le préfixe donné suivi d'un séparateur. Les
// deux nomenclatures se lisent ainsi sans que l'appelant ait à choisir : « tp1 »
// retrouve « tp1-emilie-cote », et « a26.5n6.01.tp1 » retrouve
// « a26.5n6.01.tp1.emilie-cote ».
func suit(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) {
		return false
	}
	return strings.ContainsRune(Separators, rune(name[len(prefix)]))
}

// ------------------------------------------ à qui un dépôt appartient-il ?

// Un nom de dépôt ne dit pas de façon fiable où finit le travail et où commence
// la personne : « kickmyb-firebase-Wasim7Zed » se lit aussi bien « kickmyb » +
// « firebase-Wasim7Zed ». Le découper au jugé donne alors un compte GitHub qui
// n'existe pas, et le nom qu'on rapproche ensuite est faux.
//
// Les accès, eux, ne se devinent pas : la personne qui travaille dans un dépôt
// y a été ajoutée. Son compte connu, le nom se découpe sans ambiguïté — ce qui
// reste devant est le travail, quel que soit le nombre de tirets qu'il porte.

// Split retire d'un nom de dépôt le compte qui le termine et rend le travail
// qui précède. Le compte doit finir le nom et être détaché par un séparateur :
// « kickmyb-firebase-Wasim7Zed » et « Wasim7Zed » donnent « kickmyb-firebase ».
//
// Les séparateurs en trop sont retirés avec lui. GitHub Classroom nomme parfois
// « TP3-H23-4204N6-KickMyB--alice » : deux tirets, dont un seul sépare. Garder
// le premier ferait un travail « …-KickMyB- » que rien d'autre n'écrit ainsi,
// et le même travail se dédoublerait selon que son compte a été trouvé ou non.
func Split(name, login string) (string, bool) {
	name = strings.TrimSpace(name)
	login = strings.TrimSpace(login)
	if login == "" || len(name) <= len(login)+1 {
		return "", false
	}
	coupe := len(name) - len(login)
	if !strings.EqualFold(name[coupe:], login) {
		return "", false
	}
	if !strings.ContainsRune(Separators, rune(name[coupe-1])) {
		return "", false
	}
	travail := strings.TrimRight(name[:coupe-1], Separators)
	if travail == "" {
		return "", false
	}
	return travail, true
}

// Owner choisit, parmi les comptes qui ont accès à un dépôt, celui de la
// personne à qui il appartient.
//
// Le nom tranche en premier : un dépôt qui finit par le compte de quelqu'un est
// le sien, et cela reste vrai quand l'enseignant ou un correcteur figure aussi
// dans les accès. Le compte le plus long gagne, pour que « wasim7zed » l'emporte
// sur un « akk » qui terminerait le même nom.
//
// Sans cet indice, un accès unique suffit : c'est le cas d'un dépôt que rien
// dans son nom ne rattache à personne. Au-delà, rien n'est sûr, et rien n'est
// rendu — mieux vaut le dire que se tromper.
func Owner(name string, candidates []string) (string, bool) {
	choisi := ""
	for _, candidat := range candidates {
		candidat = strings.TrimSpace(candidat)
		if _, porte := Split(name, candidat); !porte {
			continue
		}
		if len(candidat) > len(choisi) {
			choisi = candidat
		}
	}
	if choisi != "" {
		return choisi, true
	}

	uniques := make([]string, 0, len(candidates))
	vus := map[string]bool{}
	for _, candidat := range candidates {
		candidat = strings.TrimSpace(candidat)
		if candidat == "" || vus[strings.ToLower(candidat)] {
			continue
		}
		vus[strings.ToLower(candidat)] = true
		uniques = append(uniques, candidat)
	}
	if len(uniques) == 1 {
		return uniques[0], true
	}
	return "", false
}

// Resolver traduit un jeton non numérique en indice, ou renvoie -1.
type Resolver func(token string) int

var splitRe = regexp.MustCompile(`[,\s]+`)

// ParseSelection interprète « tous », « 1,3,5-8 » ou des noms, et renvoie des indices triés.
func ParseSelection(answer string, count int, resolve Resolver) ([]int, error) {
	raw := strings.TrimSpace(answer)
	if raw == "" || allKeywords[strings.ToLower(raw)] {
		all := make([]int, count)
		for index := range all {
			all[index] = index
		}
		return all, nil
	}

	chosen := map[int]bool{}
	for _, token := range splitRe.Split(raw, -1) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if span := rangeRe.FindStringSubmatch(token); span != nil {
			start, _ := strconv.Atoi(span[1])
			end, _ := strconv.Atoi(span[2])
			if start > end {
				start, end = end, start
			}
			for number := start; number <= end; number++ {
				index, err := checked(number, count, token)
				if err != nil {
					return nil, err
				}
				chosen[index] = true
			}
			continue
		}
		if number, err := strconv.Atoi(token); err == nil {
			index, err := checked(number, count, token)
			if err != nil {
				return nil, err
			}
			chosen[index] = true
			continue
		}
		index := -1
		if resolve != nil {
			index = resolve(token)
		}
		if index < 0 || index >= count {
			return nil, valid.Errorf("Sélection : « %s » ne correspond à aucune entrée de la liste.", token)
		}
		chosen[index] = true
	}

	indices := make([]int, 0, len(chosen))
	for index := range chosen {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	return indices, nil
}

func checked(number, count int, token string) (int, error) {
	if number < 1 || number > count {
		return 0, valid.Errorf("Sélection : « %s » sort de la liste (1 à %d).", token, count)
	}
	return number - 1, nil
}
