package classroom

import (
	"sort"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Un dépôt du groupe dont le dernier niveau ne mène à personne n'était qu'un
// décompte : « 11 hors liste » sous le titre d'un travail, « 11 dépôts sans
// étudiant connu » au bas de l'annuaire. Rien ne permettait d'y remédier.
//
// Le taire était pourtant le bon réflexe : inventer quelqu'un à partir d'un
// slug serait pire. Ce qui manquait est l'étape d'après — demander à GitHub si
// ce dernier niveau est un compte, et, quand il l'est, faire connaître la
// personne au registre. Le dépôt rejoint alors son étudiant sans être renommé,
// puisque le registre retient le compte comme un fragment.
//
// C'est le cas de tout ce qui a été créé avant que le registre existe, et des
// dépôts adoptés d'une organisation qui nommait par le compte.

// Orphan est un dernier niveau de nom de dépôt que rien ne rattache à
// quelqu'un — ni la liste du groupe, ni le registre de l'organisation.
type Orphan struct {
	// Fragment est le dernier niveau, tel que les dépôts le portent.
	Fragment string `json:"fragment"`
	// Repos sont ses dépôts, par ordre alphabétique.
	Repos []string `json:"repos"`
	// Assignments sont les travaux où il apparaît, dans le même ordre.
	Assignments []string `json:"assignments"`
	// PushedAt est le plus récent envoi de ses dépôts, vide s'il n'y en a eu
	// aucun : une date inventée ferait croire à une activité.
	PushedAt string `json:"pushed_at,omitempty"`
}

// Orphans relève les derniers niveaux du groupe que personne ne réclame.
//
// Ils sont rassemblés par fragment plutôt que par dépôt : une même personne
// laisse un dépôt par travail, et les rattacher un à un ferait répondre autant
// de fois à la même question. Un dépôt d'équipe n'en est pas un : il
// appartient à son équipe, et c'est normal qu'il ne nomme personne.
func (c Classroom) Orphans(repos []groups.RepoInfo, equipes []teams.Team) []Orphan {
	connus := c.fragments()
	nomsDEquipes := teamFragments(equipes)
	parFragment := map[string]*Orphan{}
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu || !naming.Belongs(parts, c.Session, c.Course, c.Group) {
			continue
		}
		cle := strings.ToLower(parts.Student)
		if nomsDEquipes[cle] {
			continue
		}
		if _, inscrit := connus.personne(parts.Student); inscrit {
			continue
		}
		orphelin, deja := parFragment[cle]
		if !deja {
			orphelin = &Orphan{Fragment: parts.Student}
			parFragment[cle] = orphelin
		}
		orphelin.Repos = append(orphelin.Repos, repo.Name)
		orphelin.Assignments = append(orphelin.Assignments, parts.Assignment)
		if envoi := valid.Moment(repo.PushedAt); envoi > orphelin.PushedAt {
			orphelin.PushedAt = envoi
		}
	}
	trouves := make([]Orphan, 0, len(parFragment))
	for _, orphelin := range parFragment {
		trouves = append(trouves, *orphelin)
	}
	return MergeOrphans(trouves)
}

// MergeOrphans rassemble des orphelins relevés séparément — d'un groupe à
// l'autre, par exemple. Un même fragment n'y figure qu'une fois, ses dépôts
// réunis : le registre est à l'échelle de l'organisation, et rattacher là
// rattache partout. C'est aussi ce qui évite de poser deux fois la même
// question sur quelqu'un qui a suivi deux cours.
func MergeOrphans(lists ...[]Orphan) []Orphan {
	parFragment := map[string]int{}
	reunis := make([]Orphan, 0)
	for _, liste := range lists {
		for _, orphelin := range liste {
			cle := strings.ToLower(orphelin.Fragment)
			position, connu := parFragment[cle]
			if !connu {
				parFragment[cle] = len(reunis)
				orphelin.Repos = append([]string(nil), orphelin.Repos...)
				orphelin.Assignments = append([]string(nil), orphelin.Assignments...)
				reunis = append(reunis, orphelin)
				continue
			}
			deja := reunis[position]
			deja.Repos = append(deja.Repos, orphelin.Repos...)
			deja.Assignments = append(deja.Assignments, orphelin.Assignments...)
			if orphelin.PushedAt > deja.PushedAt {
				deja.PushedAt = orphelin.PushedAt
			}
			reunis[position] = deja
		}
	}
	for position := range reunis {
		sort.Strings(reunis[position].Repos)
		reunis[position].Assignments = sansDoublons(reunis[position].Assignments)
	}
	sort.Slice(reunis, func(i, j int) bool {
		return strings.ToLower(reunis[i].Fragment) < strings.ToLower(reunis[j].Fragment)
	})
	return reunis
}

// sansDoublons range des travaux et n'en garde qu'un exemplaire : deux groupes
// ont chacun leur « tp1 », et le nommer deux fois n'apprendrait rien.
func sansDoublons(noms []string) []string {
	vus := map[string]bool{}
	gardes := make([]string, 0, len(noms))
	for _, nom := range noms {
		if cle := strings.ToLower(nom); !vus[cle] {
			vus[cle] = true
			gardes = append(gardes, nom)
		}
	}
	sort.Slice(gardes, func(i, j int) bool {
		return strings.ToLower(gardes[i]) < strings.ToLower(gardes[j])
	})
	return gardes
}

// FindOrphan retrouve un dernier niveau parmi ceux relevés. Ce qui n'y est pas
// n'a pas été montré : l'accepter écrirait au registre sur la foi d'une
// adresse ou d'un drapeau, et donnerait peut-être le dépôt de quelqu'un.
func FindOrphan(orphans []Orphan, fragment string) (Orphan, error) {
	cherche := strings.TrimSpace(fragment)
	for _, orphelin := range orphans {
		if strings.EqualFold(orphelin.Fragment, cherche) {
			return orphelin, nil
		}
	}
	return Orphan{}, valid.Errorf(
		"« %s » n'est le dernier niveau d'aucun dépôt sans étudiant.", cherche)
}

// ---------------------------------------------------------- rattachement

// Accounts répond à la seule question que le rattachement pose à GitHub : ce
// compte existe-t-il, et sous quel nom ?
//
// Le domaine ne connaît de l'API que cette question, comme il ne connaît du
// registre que « Names ». Un compte sans nom de profil existe quand même :
// c'est son existence qui rattache le dépôt, le nom viendra plus tard. Une
// erreur n'est pas une réponse — une panne de réseau ne dit pas qu'un compte
// manque, et le conclure ferait tenir un compte réel pour un slug.
type Accounts interface {
	Account(login string) (person roster.Person, exists bool, err error)
}

// Found est un orphelin dont le dernier niveau est un compte GitHub.
type Found struct {
	Orphan Orphan        `json:"orphan"`
	Person roster.Person `json:"person"`
}

// Adoption est ce qu'un rattachement ferait, avant qu'il ne le fasse.
type Adoption struct {
	// Found sont les orphelins que GitHub reconnaît, un par fragment.
	Found []Found `json:"found"`
	// Unknown sont ceux qui ne mènent à aucun compte : un slug de nom, un
	// compte fermé depuis. Ce sont eux qui resteront à rattacher à la main.
	Unknown []Orphan `json:"unknown"`
}

// Ready dit qu'il y a quelque chose à écrire.
func (a Adoption) Ready() bool { return len(a.Found) > 0 }

// People rend les personnes que le registre apprendra.
func (a Adoption) People() []roster.Person {
	personnes := make([]roster.Person, 0, len(a.Found))
	for _, trouve := range a.Found {
		personnes = append(personnes, trouve.Person)
	}
	return personnes
}

// Repos compte les dépôts que le rattachement rendra à leur étudiant.
func (a Adoption) Repos() int {
	total := 0
	for _, trouve := range a.Found {
		total += len(trouve.Orphan.Repos)
	}
	return total
}

// Only ne garde que les fragments retenus. Un compte GitHub qui porte le slug
// d'un nom n'est pas forcément celui de l'étudiant : c'est à qui regarde le
// nom du profil d'en juger, et ce qu'il écarte ne doit pas s'écrire.
func (a Adoption) Only(fragments []string) Adoption {
	retenus := map[string]bool{}
	for _, fragment := range fragments {
		retenus[strings.ToLower(strings.TrimSpace(fragment))] = true
	}
	gardee := Adoption{Found: make([]Found, 0, len(a.Found)), Unknown: a.Unknown}
	for _, trouve := range a.Found {
		if retenus[strings.ToLower(trouve.Orphan.Fragment)] {
			gardee.Found = append(gardee.Found, trouve)
		}
	}
	return gardee
}

// PlanAdoption trie les orphelins selon ce que GitHub sait d'eux.
//
// Rien n'est deviné : un fragment qui n'est pas un compte reste un fragment.
// Le rapprocher d'une personne par ressemblance a sa place à la reprise, où
// l'on a une liste en face ; ici il n'y en a pas, et se tromper rattacherait le
// dépôt de quelqu'un à quelqu'un d'autre.
func PlanAdoption(orphans []Orphan, comptes Accounts) (Adoption, error) {
	plan := Adoption{Found: make([]Found, 0), Unknown: make([]Orphan, 0)}
	for _, orphelin := range orphans {
		person, existe, err := compteDe(comptes, orphelin.Fragment)
		if err != nil {
			return plan, err
		}
		if !existe {
			plan.Unknown = append(plan.Unknown, orphelin)
			continue
		}
		plan.Found = append(plan.Found, Found{Orphan: orphelin, Person: person})
	}
	return plan, nil
}

// compteDe demande à GitHub si un dernier niveau est un compte.
//
// La marque que GitHub ajoute à un nom déjà pris n'en fait pas un autre
// compte : « dtroi-1 » est « dtroi », et c'est lui qu'il faut aller demander.
// Le registre fait ensuite le même chemin pour rattacher le dépôt.
func compteDe(comptes Accounts, login string) (roster.Person, bool, error) {
	if comptes == nil {
		return roster.Person{}, false, nil
	}
	person, existe, err := comptes.Account(login)
	if err != nil || existe {
		return compteRendu(person, login), existe, err
	}
	base, marque := roster.WithoutDuplicateMarker(login)
	if !marque {
		return roster.Person{}, false, nil
	}
	person, existe, err = comptes.Account(base)
	return compteRendu(person, base), existe, err
}

// compteRendu garde le compte tel que GitHub l'écrit : sa casse est la bonne,
// et une marque de doublon n'y figure pas.
func compteRendu(person roster.Person, demande string) roster.Person {
	if strings.TrimSpace(person.Username) == "" {
		person.Username = strings.TrimSpace(demande)
	}
	person.FullName = strings.TrimSpace(person.FullName)
	return person
}

// AttachRequest décrit un rattachement fait à la main : ce dépôt-là est celui
// de ce compte-là.
type AttachRequest struct {
	// Fragment est le dernier niveau à rattacher.
	Fragment string
	// Login est le compte de la personne. Vide, c'est le dernier niveau
	// lui-même — le cas courant, celui que GitHub reconnaît.
	Login string
	// FullName est le nom à lui donner. Vide, il vient de ce que le registre
	// sait déjà d'elle, ou à défaut de son profil GitHub.
	FullName string
}

// PlanAttach compose un rattachement fait à la main.
//
// C'est ce qui reste quand le dernier niveau n'est pas un compte : un slug de
// nom, celui de quelqu'un dont on connaît le compte par ailleurs. Le compte
// donné est vérifié — sur le registre d'abord, qui le connaît peut-être sous
// un autre nom, sur GitHub ensuite — : une faute de frappe y laisserait sinon
// une fiche que rien ne désigne.
func PlanAttach(orphans []Orphan, demande AttachRequest, connus Names,
	comptes Accounts) (Found, error) {
	orphelin, err := FindOrphan(orphans, demande.Fragment)
	if err != nil {
		return Found{}, err
	}
	login := strings.TrimSpace(demande.Login)
	if login == "" {
		login = orphelin.Fragment
	}
	if login, err = valid.Login(login, "Compte GitHub"); err != nil {
		return Found{}, err
	}

	person, existe := roster.Person{}, false
	if connus != nil {
		person, existe = connus.Lookup(login)
		// Le registre répond aussi à un slug : seul un compte qui y est le
		// sien désigne la personne qu'on attend.
		existe = existe && strings.EqualFold(person.Username, login)
	}
	if !existe {
		if person, existe, err = compteDe(comptes, login); err != nil {
			return Found{}, err
		}
	}
	if !existe {
		return Found{}, valid.Errorf("Le compte « %s » n'existe pas sur GitHub.", login)
	}
	if nom := strings.TrimSpace(demande.FullName); nom != "" {
		if person.FullName, err = valid.FullName(nom); err != nil {
			return Found{}, err
		}
	}
	return Found{Orphan: orphelin, Person: person}, nil
}
