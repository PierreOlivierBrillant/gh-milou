package classroom

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// Une personne change de groupe en cours de session, ou son nom était mal
// orthographié : c'est fréquent. Sa fiche suit toujours ; ses dépôts, eux, ne
// bougent que si on le demande — les renommer est une écriture sur GitHub, pas
// un rangement local.
//
// Un travail entier se déplace aussi, et pour une autre raison. Une
// organisation adoptée sans convention range parfois sous un même préfixe —
// « travail-de » — les travaux de plusieurs groupes et de plusieurs sessions.
// Les séparer demande de sortir un travail de ce fourre-tout, et rien ne dit
// qui sont ses étudiants : c'est justement ce qu'on cherche à établir.
//
// D'où la règle qui tient tout : **le dernier niveau du nom est conservé tel
// quel quand rien ne permet de faire mieux**. Le dépôt arrive à la bonne place
// sous le fragment qu'il portait — souvent un compte GitHub —, le groupe le
// reconnaît quand même (voir « knownBy »), et le nom complet se corrige ensuite,
// une fois le groupe à la nomenclature courante. Sans cela, déplacer réclamerait
// un nom complet, et le retrouver réclamerait un groupe déplacé.
//
// Le plan se compose entièrement avant que le premier renommage ne parte : une
// collision refuse ainsi l'opération au lieu de l'interrompre à mi-chemin.

// Move est un dépôt à renommer parce que son étudiant a changé de groupe ou de
// nom, ou parce que son travail change de place.
type Move struct {
	Repo     string `json:"repo"`
	Target   string `json:"target"`
	Student  string `json:"student"`
	Username string `json:"username"`
}

// PlanMove compose le renommage des dépôts des personnes qui passent d'un
// groupe à l'autre. Un groupe d'arrivée qui ne suit pas la nomenclature
// courante ne sait pas nommer : le déplacement se fait alors sans les dépôts.
func PlanMove(depart, arrivee Classroom, personnes []roster.Person,
	repos []groups.RepoInfo) ([]Move, error) {
	// Une personne est retrouvée par tout ce qui la désigne, comme Without la
	// retire : son dépôt se rattache à la ligne qui porte son nom, et ce n'est
	// pas toujours celle de son compte. Ce qui quitte la liste et ce qui suit
	// sur GitHub doivent être la même chose.
	fragments := map[string]string{}
	for _, personne := range personnes {
		for _, cle := range designationsDe(personne) {
			fragments[cle] = fragmentDe(personne)
		}
	}
	return planRenames(depart, arrivee, fragments, repos)
}

// Movers rassemble les personnes du groupe que ces mots désignent, pour les
// déplacer. Chaque mot désigne comme Find : un compte ou, à défaut, un
// matricule — c'est ainsi que se choisit qui n'a encore aucun compte. Une
// personne n'y figure qu'une fois, quel que soit le nombre de mots qui la
// désignent, et dans l'ordre où on les a nommées.
//
// Un mot que le groupe ne connaît pas refuse le tout : un déplacement ne part
// pas avec une partie de ce qu'on a choisi. Figurer déjà dans le groupe
// d'arrivée, en revanche, ne refuse rien : c'est même la situation qu'on vient
// réparer. Une erreur d'inscription laisse quelqu'un avec des dépôts dans deux
// groupes, et le déplacement est justement ce qui les rassemble — ses dépôts
// rejoignent l'arrivée, sa fiche quitte le départ et retrouve celle qui l'y
// attendait.
func (c Classroom) Movers(handles []string) ([]roster.Person, error) {
	identites := c.Identities()
	choisies := make([]bool, len(identites))
	personnes := make([]roster.Person, 0, len(handles))
	for _, handle := range handles {
		handle = strings.TrimSpace(handle)
		if handle == "" {
			continue
		}
		position, trouvee := rang(identites, handle)
		if !trouvee {
			// Le mot peut être un matricule : lui prêter une arobase en ferait
			// un compte qu'il n'est pas.
			return nil, valid.Errorf("« %s » n'est pas dans « %s ».", handle, c.Label())
		}
		if choisies[position] {
			continue
		}
		choisies[position] = true
		personnes = append(personnes, identites[position].Person())
	}
	if len(personnes) == 0 {
		return nil, valid.Errorf("Aucune personne à déplacer.")
	}
	return personnes, nil
}

// PlanRenameStudent compose le renommage des dépôts d'une personne dont le nom
// complet change. Le nom de l'étudiant est le dernier niveau du nom d'un dépôt :
// corriger sa fiche ne suffit pas, les dépôts déjà créés portent l'ancien.
//
// La personne est désignée par ce qui la désignait — le compte qu'elle avait, à
// défaut son matricule : c'est ce qui la retrouve dans le groupe tel qu'il est
// encore.
func PlanRenameStudent(cours Classroom, avant, apres roster.Person,
	repos []groups.RepoInfo) ([]Move, error) {
	return planRenames(cours, cours,
		map[string]string{Designation(avant): fragmentDe(apres)}, repos)
}

// Correction est la fiche corrigée d'une personne du groupe : ce qu'elle était,
// ce qu'elle devient, le groupe qui la porte désormais, et les dépôts à
// renommer pour qu'ils suivent — aucun quand on ne l'a pas demandé.
type Correction struct {
	Before    roster.Person
	After     roster.Person
	Classroom Classroom
	Moves     []Move
}

// Changed dit que la fiche elle-même change : son nom complet ou son compte.
// La casse d'un compte ne compte pas, GitHub ne la distingue pas.
func (c Correction) Changed() bool {
	return c.Before.FullName != c.After.FullName ||
		!strings.EqualFold(c.Before.Username, c.After.Username)
}

// PlanCorrection compose la correction de la fiche d'une personne du groupe —
// son nom complet, son compte GitHub, ou les deux — et, si on le demande, le
// renommage de ses dépôts pour qu'ils portent son nom. Rien n'est écrit ici :
// les trois interfaces partent de ce plan, et refusent donc les mêmes choses.
//
// Un nom ou un compte laissé vide reste ce qu'il était. La fiche corrigée part
// de la ligne de la liste plutôt que de rien : son matricule et ses autres
// comptes sont à elle, et une correction de nom n'a pas à les lui faire perdre.
//
// Le groupe doit avoir été enrichi de l'inventaire : c'est lui qui rattache à
// la personne les dépôts nommés d'après un ancien nom.
func PlanCorrection(cours Classroom, username string, wanted roster.Person,
	withRepos bool, repos []groups.RepoInfo) (Correction, error) {
	avant, inscrit := cours.Find(username)
	if !inscrit {
		return Correction{}, valid.Errorf("@%s n'est pas dans « %s ».",
			strings.TrimSpace(username), cours.Label())
	}
	apres := avant
	for _, student := range cours.Students {
		if student.Owns(avant.Username) {
			apres = student
			break
		}
	}
	if nom := strings.TrimSpace(wanted.FullName); nom != "" {
		apres.FullName = nom
	}
	if compte := strings.TrimSpace(wanted.Username); compte != "" {
		apres.Username = compte
	}
	// Qui n'a pas encore de compte — toute une cohorte importée du collège —
	// n'est désigné que par son matricule : c'est lui qui retrouve sa ligne, et
	// c'est ainsi qu'on lui rattache son premier compte. Son compte vide, lui,
	// ne désignerait personne.
	modifie, err := cours.Rename(Handle(avant), apres)
	if err != nil {
		return Correction{}, err
	}
	apres, _ = modifie.Find(Handle(apres))
	correction := Correction{Before: avant, After: apres, Classroom: modifie}
	if withRepos {
		if correction.Moves, err = PlanRenameStudent(cours, avant, apres, repos); err != nil {
			return Correction{}, err
		}
	}

	// Une correction qui ne change rien n'a rien à écrire : la laisser passer
	// ferait croire à un geste qui n'a pas eu lieu.
	if !correction.Changed() && len(correction.Moves) == 0 {
		if withRepos {
			return Correction{}, valid.Errorf("Rien à corriger : ni le nom ni le compte de "+
				"@%s n'ont changé, et ses dépôts portent déjà son nom.", avant.Username)
		}
		return Correction{}, valid.Errorf(
			"Ni le nom ni le compte de @%s n'ont changé.", avant.Username)
	}
	return correction, nil
}

// fragmentDe rend le fragment qui nomme une personne dans un dépôt. Il est vide
// quand son nom complet manque : le dépôt gardera alors celui qu'il porte.
func fragmentDe(personne roster.Person) string {
	fragment, err := naming.Student(personne.FullName)
	if err != nil {
		return ""
	}
	return fragment
}

// planRenames compose les nouveaux noms : chaque dépôt du groupe de départ dont
// l'étudiant figure dans « fragments », sous sa désignation, rejoint la place
// d'arrivée sous le fragment qui lui est associé. Départ et arrivée peuvent
// être le même groupe — c'est le cas quand seul le nom de la personne change.
//
// La désignation, et non le compte seul : deux personnes sans compte n'ont en
// commun qu'un compte vide, et l'une emporterait les dépôts de l'autre — sous
// son propre nom.
func planRenames(depart, arrivee Classroom, fragments map[string]string,
	repos []groups.RepoInfo) ([]Move, error) {
	pris := nouvellesCibles(repos)

	var lignes []Move
	// Les équipes ne sont pas nécessaires ici : seuls les dépôts d'étudiants
	// suivent une personne qui change de groupe ou de nom. Ceux d'une équipe
	// appartiennent à l'équipe, et elle ne bouge pas.
	for _, travail := range depart.Assignments(repos, nil) {
		nom, err := naming.Fragment(depart.ShortName(travail.ID), "Travail")
		if err != nil {
			continue
		}
		for _, repo := range depart.Repos(travail.ID, repos) {
			student, inscrit := depart.StudentOf(repo.Name)
			if !inscrit {
				continue
			}
			cle := Designation(student)
			fragment, concerne := fragments[cle]
			if cle == "" || !concerne {
				continue
			}
			ligne, err := pris.viser(arrivee, nom, repo, fragment, student.Username)
			if err != nil {
				return nil, err
			}
			if ligne != nil {
				lignes = append(lignes, *ligne)
			}
		}
	}
	return lignes, nil
}

// PlanMoveTeam compose le renommage des dépôts qu'une équipe emporte en
// changeant de groupe : les siens et ceux de ses membres.
//
// Une équipe appartient à un groupe — son nom le dit, et c'est ce qui permet à
// deux groupes d'avoir chacun leur « eq1 ». La déplacer, c'est donc déplacer
// les trois choses à la fois : l'équipe, les gens qui la composent, et tout ce
// que les uns comme l'autre ont rendu. Rien ne resterait cohérent autrement :
// des dépôts d'un groupe appartenant à une équipe d'un autre.
//
// Un dépôt d'équipe garde son nom court à l'arrivée ; celui d'un étudiant garde
// le fragment qui le nomme, ou celui qu'il porte déjà quand son nom complet
// manque encore.
func PlanMoveTeam(depart, arrivee Classroom, equipe teams.Team,
	membres []roster.Person, repos []groups.RepoInfo) ([]Move, error) {
	// Une personne est indexée sous tous ses comptes : le dépôt peut être
	// nommé d'après l'un, et l'équipe la porter sous l'autre.
	fragments := map[string]string{}
	for _, personne := range membres {
		for _, compte := range personne.Accounts() {
			fragments[strings.ToLower(compte)] = fragmentDe(personne)
		}
	}
	seule := []teams.Team{equipe}
	pris := nouvellesCibles(repos)

	var lignes []Move
	for _, travail := range depart.Assignments(repos, seule) {
		nom, err := naming.Fragment(depart.ShortName(travail.ID), "Travail")
		if err != nil {
			continue
		}
		for _, repo := range depart.Repos(travail.ID, repos) {
			fragment, username := "", ""
			if _, sien := depart.TeamOf(repo.Name, seule); !sien {
				student, inscrit := depart.StudentOf(repo.Name)
				if !inscrit {
					continue
				}
				attendu, concerne := fragments[strings.ToLower(student.Username)]
				if !concerne {
					continue
				}
				fragment, username = attendu, student.Username
			}
			// Le dépôt de l'équipe n'a pas de fragment à viser : le sien la
			// nomme déjà, et « viser » le reprend tel quel.
			ligne, err := pris.viser(arrivee, nom, repo, fragment, username)
			if err != nil {
				return nil, err
			}
			if ligne != nil {
				lignes = append(lignes, *ligne)
			}
		}
	}
	return lignes, nil
}

// ------------------------------------------------------ déplacer des travaux

// Relocation désigne un travail à déplacer et le nom qu'il portera à l'arrivée.
// Un nom laissé vide garde celui qu'il a — mais un travail tiré d'un préfixe
// fourre-tout mérite souvent mieux, et c'est le seul moment où le lui donner ne
// coûte rien.
type Relocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PlanMoveAssignments compose le renommage des dépôts des travaux qui passent
// d'un groupe à un autre. Contrairement au déplacement d'une personne, il ne
// demande de connaître personne : un dépôt dont l'étudiant reste inconnu garde
// le fragment qu'il porte, et arrive quand même à la bonne place.
func PlanMoveAssignments(depart, arrivee Classroom, travaux []Relocation,
	repos []groups.RepoInfo) ([]Move, error) {
	if len(travaux) == 0 {
		return nil, valid.Errorf("Aucun travail à déplacer.")
	}
	connus := knownBy(append(append([]roster.Person(nil), depart.Students...),
		arrivee.Students...))
	pris := nouvellesCibles(repos)

	var lignes []Move
	for _, demande := range travaux {
		depots := depart.Repos(demande.ID, repos)
		if len(depots) == 0 {
			return nil, valid.Errorf("Aucun dépôt pour le travail « %s ».",
				depart.ShortName(demande.ID))
		}
		nom := strings.TrimSpace(demande.Name)
		if nom == "" {
			nom = depart.ShortName(demande.ID)
		}
		partantes, err := pris.travail(arrivee, nom, depots, connus)
		if err != nil {
			return nil, err
		}
		lignes = append(lignes, partantes...)
	}
	return lignes, nil
}

// PlanRelocate compose le renommage d'un lot de dépôts vers une place et un nom
// de travail donnés. C'est le même déplacement, dit sans groupe de départ :
// l'assistant du terminal ne connaît que des préfixes — un travail y est un
// préfixe et ses dépôts —, et il lui faut bien pouvoir en sortir un aussi.
//
// « connus » peut être vide : les dépôts gardent alors tous le dernier niveau
// de leur nom.
func PlanRelocate(arrivee Classroom, travail string, depots []groups.Repo,
	connus []roster.Person, repos []groups.RepoInfo) ([]Move, error) {
	if len(depots) == 0 {
		return nil, valid.Errorf("Aucun dépôt à déplacer.")
	}
	return nouvellesCibles(repos).travail(arrivee, travail, depots, knownBy(connus))
}

// Followers dit qui suit un déplacement de travaux, et qui quitte le groupe de
// départ. Une personne suit dès qu'un des dépôts qui partent est le sien ; elle
// ne quitte le départ que s'il ne lui en reste aucun — sinon elle serait
// inscrite là où elle n'a plus rien, et absente là où elle a encore quelque
// chose.
//
// Un dépôt est à la personne que sa ligne désigne, compte ou matricule. Le
// compte seul ne suffit pas : vide, il est commun à tous ceux qui n'en ont pas
// encore, et l'un suivait alors les dépôts de l'autre.
func Followers(depart Classroom, plan []Move, repos []groups.RepoInfo) (
	suivent []roster.Person, quittent []roster.Person) {
	partants := map[string]bool{}
	for _, ligne := range plan {
		partants[strings.ToLower(ligne.Repo)] = true
	}

	total := map[string]int{}
	emportes := map[string]int{}
	for _, travail := range depart.Assignments(repos, nil) {
		for _, depot := range depart.Repos(travail.ID, repos) {
			student, inscrit := depart.StudentOf(depot.Name)
			if !inscrit {
				continue
			}
			cle := Designation(student)
			total[cle]++
			if partants[strings.ToLower(depot.Name)] {
				emportes[cle]++
			}
		}
	}

	for _, personne := range depart.Students {
		cle := Designation(personne)
		if cle == "" || emportes[cle] == 0 {
			continue
		}
		suivent = append(suivent, personne)
		if emportes[cle] >= total[cle] {
			quittent = append(quittent, personne)
		}
	}
	return suivent, quittent
}

// ------------------------------------------------------------------- cibles

// cibles retient les noms déjà portés et ceux que le plan vise déjà. Les deux
// se valent : GitHub refuserait le renommage dans un cas, et le plan se
// heurterait à lui-même dans l'autre.
type cibles struct {
	existants map[string]bool
	vises     map[string]string
}

func nouvellesCibles(repos []groups.RepoInfo) *cibles {
	pris := &cibles{
		existants: make(map[string]bool, len(repos)),
		vises:     map[string]string{},
	}
	for _, repo := range repos {
		pris.existants[strings.ToLower(repo.Name)] = true
	}
	return pris
}

// travail compose le renommage des dépôts d'un travail entier. Les noms visés
// restent retenus d'un appel à l'autre : deux travaux déplacés ensemble ne
// doivent pas se heurter.
func (c *cibles) travail(arrivee Classroom, nom string, depots []groups.Repo,
	connus known) ([]Move, error) {
	fragment, err := naming.Fragment(nom, "Travail")
	if err != nil {
		return nil, err
	}
	lignes := make([]Move, 0, len(depots))
	for _, depot := range depots {
		personne, _ := connus.personne(depot.Suffix)
		ligne, err := c.viser(arrivee, fragment, depot,
			fragmentDe(personne), personne.Username)
		if err != nil {
			return nil, err
		}
		if ligne != nil {
			lignes = append(lignes, *ligne)
		}
	}
	return lignes, nil
}

// viser compose le nom d'arrivée d'un dépôt et le retient. Un fragment vide dit
// que rien de mieux n'est connu de l'étudiant : le dépôt garde le sien. Un
// dépôt qui porte déjà le nom visé n'a rien à faire dans le plan — il se
// heurterait à lui-même —, et rend une ligne nulle.
func (c *cibles) viser(arrivee Classroom, travail string, depot groups.Repo,
	fragment, username string) (*Move, error) {
	if fragment == "" {
		reprise, err := naming.Fragment(depot.Suffix, "Étudiant")
		if err != nil {
			return nil, valid.Errorf(
				"« %s » : le dernier niveau de son nom ne peut pas être repris. %v",
				depot.Name, err)
		}
		fragment = reprise
	}
	cible := naming.Compose(arrivee.Session, arrivee.Course, arrivee.Group,
		travail, fragment)
	if strings.EqualFold(cible, depot.Name) {
		return nil, nil
	}
	cle := strings.ToLower(cible)
	if precedent := c.vises[cle]; precedent != "" {
		return nil, valid.Errorf("« %s » et « %s » viseraient le même nom « %s ».",
			precedent, depot.Name, cible)
	}
	if c.existants[cle] {
		return nil, valid.Errorf("« %s » existe déjà.", cible)
	}
	c.vises[cle] = depot.Name
	return &Move{
		Repo: depot.Name, Target: cible, Student: fragment, Username: username,
	}, nil
}

// ------------------------------------------------------------------- listes

// Without rend le groupe privé des personnes données. Une ligne part quand ce
// qui la désigne — son compte, à défaut son matricule — est l'une des
// désignations de ces personnes. Telle que Find ou Movers la rend, une personne
// les porte toutes, et part donc avec toutes ses lignes : en laisser une
// laisserait au départ la moitié de quelqu'un qui travaille sous deux comptes,
// et retirer par compte seul y laissait tous ceux qui n'en ont pas encore.
//
// Les autres comptes qu'une ligne déclare n'y comptent pas : retirer un compte
// ne retire pas la personne qui le compte parmi les siens.
func (c Classroom) Without(people ...roster.Person) Classroom {
	partantes := map[string]bool{}
	for _, personne := range people {
		for _, cle := range designationsDe(personne) {
			partantes[cle] = true
		}
	}
	restants := make([]roster.Person, 0, len(c.Students))
	for _, ligne := range c.Students {
		if partantes[Designation(ligne)] {
			continue
		}
		restants = append(restants, ligne)
	}
	c.Students = restants
	return c
}

// With rend le groupe augmenté des personnes données. Qui y est déjà inscrit
// sous la même désignation — son compte, à défaut son matricule — ne s'y
// inscrit pas une seconde fois, et deux personnes sans compte y restent deux
// personnes (voir dedupe).
func (c Classroom) With(people ...roster.Person) Classroom {
	c.Students = dedupe(append(append([]roster.Person(nil), c.Students...), people...))
	return c
}

// ------------------------------------------------------ renommer un travail

// PlanRenameAssignment compose le renommage des dépôts d'un travail qui change
// de nom sans changer de place. Le nom du travail est un niveau du nom de
// chaque dépôt : il n'y a pas de fiche où le corriger — les dépôts sont tout ce
// qu'un travail est.
//
// Le dernier niveau est repris tel quel, même quand le nom complet de
// l'étudiant est connu : on renomme le travail, pas les personnes. Corriger un
// nom mal orthographié a sa propre opération, et mêler les deux rendrait
// illisible le renommage qu'on montre avant d'écrire.
func PlanRenameAssignment(cours Classroom, id, nom string,
	repos []groups.RepoInfo) ([]Move, error) {
	fragment, err := naming.Fragment(nom, "Travail")
	if err != nil {
		return nil, err
	}
	depots := cours.Repos(id, repos)
	if len(depots) == 0 {
		return nil, valid.Errorf("Aucun dépôt pour le travail « %s ».", cours.ShortName(id))
	}
	lignes, err := nouvellesCibles(repos).travail(cours, fragment, depots, nil)
	if err != nil {
		return nil, err
	}
	if len(lignes) == 0 {
		return nil, valid.Errorf("« %s » porte déjà ce nom.", fragment)
	}
	return lignes, nil
}
