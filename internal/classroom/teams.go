package classroom

import (
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/naming"
	"github.com/PierreOlivierBrillant/gh-milou/internal/plan"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

// Un travail d'équipe, c'est un dépôt par équipe au lieu d'un dépôt par
// personne. Rien d'autre ne change : même nomenclature, même gabarit de nom,
// même plan. Le dernier niveau du nom porte l'équipe — « a26.5n6.01.tp1.eq1 » —
// là où un travail individuel porte l'étudiant.
//
// C'est ce qui permet de lire la nature d'un travail sans l'avoir déclarée
// nulle part : le dernier niveau nomme une équipe du groupe, ou il n'en nomme
// pas. Un travail fait en équipe avant l'arrivée de l'outil s'adopte donc en
// adoptant ses équipes — les dépôts, eux, n'ont rien à changer.

// Teams retient les équipes du groupe parmi celles de l'organisation.
func (c Classroom) Teams(infos []teams.Info) []teams.Team {
	return teams.In(c.Session, c.Course, c.Group, infos)
}

// TeamName compose le nom complet d'une équipe du groupe.
func (c Classroom) TeamName(short string) string {
	return naming.TeamName(c.Session, c.Course, c.Group, strings.TrimSpace(short))
}

// TeamMovers rassemble les fiches que l'équipe emporte quand elle change de
// groupe. Un compte que la liste du groupe ne connaît pas n'a pas de fiche à
// déplacer : l'équipe le garde quand même, puisque c'est elle qui le porte.
func (c Classroom) TeamMovers(equipe teams.Team) []roster.Person {
	vus := map[string]bool{}
	partants := make([]roster.Person, 0, len(equipe.Members))
	for _, compte := range equipe.Members {
		personne, inscrit := c.Find(compte)
		if !inscrit || vus[strings.ToLower(personne.Username)] {
			continue
		}
		vus[strings.ToLower(personne.Username)] = true
		partants = append(partants, personne)
	}
	SortPeople(partants)
	return partants
}

// TeamRepos nomme les dépôts que l'équipe a rendus, tous travaux confondus.
// Ce sont ceux dont le dernier niveau la nomme, et eux seuls : un dépôt qu'elle
// voit sans le nommer est celui de quelqu'un d'autre, et il lui survit.
func (c Classroom) TeamRepos(equipe teams.Team, repos []groups.RepoInfo) []string {
	seule := []teams.Team{equipe}
	sien := make([]string, 0, 2)
	for _, depot := range repos {
		if _, aElle := c.TeamOf(depot.Name, seule); aElle {
			sien = append(sien, depot.Name)
		}
	}
	return sien
}

// TeamOf retrouve l'équipe à laquelle un dépôt du groupe appartient.
func (c Classroom) TeamOf(repoName string, equipes []teams.Team) (teams.Team, bool) {
	parts, reconnu := naming.Parse(repoName)
	if !reconnu || !naming.Belongs(parts, c.Session, c.Course, c.Group) {
		return teams.Team{}, false
	}
	return teams.Find(equipes, parts.Student)
}

// ServedTeams renvoie les équipes qui ont déjà un dépôt pour ce travail. C'est
// l'équivalent de « Served » pour un travail d'équipe : ce qui a déjà été
// distribué ne l'est pas deux fois.
func (c Classroom) ServedTeams(assignmentID string, repos []groups.RepoInfo,
	equipes []teams.Team) map[string]bool {
	servis := map[string]bool{}
	for _, repo := range repos {
		parts, reconnu := naming.Parse(repo.Name)
		if !reconnu {
			continue
		}
		id := naming.AssignmentID(parts.Session, parts.Course, parts.Group, parts.Assignment)
		if !strings.EqualFold(id, assignmentID) {
			continue
		}
		if equipe, connue := teams.Find(equipes, parts.Student); connue {
			servis[strings.ToLower(equipe.Short)] = true
		}
	}
	return servis
}

// TeamTargets met les équipes sous la forme que le plan attend.
func TeamTargets(equipes []teams.Team) []plan.TeamTarget {
	cibles := make([]plan.TeamTarget, 0, len(equipes))
	for _, equipe := range equipes {
		cibles = append(cibles, plan.TeamTarget{
			Short: equipe.Short, Slug: equipe.Slug, Members: equipe.Members,
		})
	}
	return cibles
}

// TeamRoster décrit une équipe pour l'affichage : ses membres nommés d'après la
// liste du groupe, ses enseignants, et ceux qui ne sont ni l'un ni l'autre.
// Une équipe peut contenir quelqu'un qui a quitté le cours, ou que la liste
// n'a jamais connu : le taire laisserait croire que l'équipe est plus petite
// qu'elle n'est.
type TeamRoster struct {
	teams.Team
	// People nomme les membres que la liste du groupe connaît.
	People []roster.Person `json:"people"`
	// Waiting nomme ceux, parmi tous, qui n'ont pas encore accepté leur
	// invitation. Ils sont bien de l'équipe : ils ne l'ont pas encore su.
	Waiting []string `json:"waiting,omitempty"`
	// Teachers nomme les membres que le registre déclare enseignants. Ils ne
	// sont pas de la liste sans être des inconnus pour autant : les mêler aux
	// autres inviterait à inscrire au groupe, comme étudiant, celui qui
	// l'enseigne.
	Teachers []roster.Person `json:"teachers"`
	// Strangers rassemble les comptes de l'équipe que rien ne rattache au
	// groupe. Ils portent le nom que le registre de l'organisation leur donne,
	// quand il en connaît un : un compte qu'on sait nommer doit être nommé,
	// même s'il n'est inscrit nulle part.
	Strangers []roster.Person `json:"strangers"`
}

// Describe habille les équipes du groupe de ce que sa liste et le registre de
// l'organisation savent de leurs membres, dans l'ordre des équipes. Le
// registre nomme ceux que la liste ne connaît pas ; sans lui, ils n'ont que
// leur compte. Qui enseigne, c'est ce que « Staffing » a versé dans le groupe :
// un groupe qui n'en sait rien range ses enseignants parmi les inconnus.
func (c Classroom) Describe(equipes []teams.Team, names Names) []TeamRoster {
	nommes := c.Named()
	nommer := func(compte string) string {
		if nom := nommes[strings.ToLower(compte)]; nom != "" {
			return nom
		}
		if names != nil {
			if connue, trouvee := names.Lookup(compte); trouvee {
				return connue.FullName
			}
		}
		return ""
	}
	fiches := make([]TeamRoster, 0, len(equipes))
	for _, equipe := range equipes {
		fiche := TeamRoster{
			Team:      equipe,
			People:    make([]roster.Person, 0, len(equipe.Members)),
			Teachers:  make([]roster.Person, 0),
			Strangers: make([]roster.Person, 0),
		}
		// Une personne dont deux comptes sont dans l'équipe n'y est qu'une
		// fois : elle y figurerait sinon deux fois sous le même nom, ce qui se
		// lit comme deux personnes.
		vues := map[string]bool{}
		for _, membre := range equipe.Members {
			// La liste passe avant le registre : quelqu'un qu'elle porte est de
			// l'équipe à ce titre, qu'il enseigne ailleurs ou non.
			personne, inscrit := c.Find(membre)
			if !inscrit {
				hors := roster.Person{FullName: nommer(membre), Username: membre}
				if c.enseigne(membre) {
					fiche.Teachers = append(fiche.Teachers, hors)
				} else {
					fiche.Strangers = append(fiche.Strangers, hors)
				}
				continue
			}
			if vues[strings.ToLower(personne.Username)] {
				continue
			}
			vues[strings.ToLower(personne.Username)] = true
			// Les comptes retenus sont ceux par lesquels elle est dans
			// l'équipe : c'est ce qu'on retire en l'en retirant.
			fiche.People = append(fiche.People, roster.Person{
				FullName: personne.FullName, Username: membre,
				Also: autresDansLEquipe(personne, equipe, membre),
			})
		}
		SortPeople(fiche.People)
		SortPeople(fiche.Teachers)
		SortPeople(fiche.Strangers)
		for _, membre := range equipe.Members {
			if equipe.Waiting(membre) {
				fiche.Waiting = append(fiche.Waiting, membre)
			}
		}
		fiches = append(fiches, fiche)
	}
	return fiches
}

// Members nomme ceux que le dépôt d'une équipe concerne : leur nom complet quand
// le groupe ou le registre le connaît, leur compte sinon. Un dépôt d'équipe ne
// porte le nom de personne ; dire qui il concerne n'a d'intérêt qu'en le disant
// par leur nom.
//
// Un enseignant que la liste du groupe ne porte pas n'en est pas, même inscrit
// à l'équipe : le dépôt ne lui est pas destiné, et son silence s'y lirait comme
// celui d'un étudiant. La composition entière, enseignants compris, c'est
// « Describe » qui la donne.
func (c Classroom) Members(equipe teams.Team) []roster.Person {
	nommes := c.Named()
	membres := make([]roster.Person, 0, len(equipe.Members))
	for _, compte := range equipe.Members {
		personne, inscrit := c.Find(compte)
		if !inscrit && c.enseigne(compte) {
			continue
		}
		nom := nommes[strings.ToLower(compte)]
		if inscrit && personne.FullName != "" {
			nom = personne.FullName
		}
		membres = append(membres, roster.Person{FullName: nom, Username: compte})
	}
	SortPeople(membres)
	return membres
}

// enseigne dit qu'un compte est déclaré enseignant. Un groupe que « Staffing »
// n'a pas branché sur le registre ne le sait pas, et ne le devine pas.
func (c Classroom) enseigne(compte string) bool {
	return c.enseignants != nil && c.enseignants.Teaches(compte)
}

// autresDansLEquipe rend les autres comptes d'une personne que l'équipe porte
// aussi. Les taire ferait croire qu'en retirer un suffit à l'en sortir.
func autresDansLEquipe(personne roster.Person, equipe teams.Team,
	sauf string) []string {
	autres := make([]string, 0, 1)
	for _, compte := range personne.Accounts() {
		if strings.EqualFold(compte, sauf) || !equipe.Has(compte) {
			continue
		}
		autres = append(autres, compte)
	}
	if len(autres) == 0 {
		return nil
	}
	return autres
}

// Unassigned renvoie les étudiants du groupe qui ne sont dans aucune équipe :
// ceux qu'un travail d'équipe laisserait de côté.
func (c Classroom) Unassigned(equipes []teams.Team) []roster.Person {
	restants := make([]roster.Person, 0)
	for _, identite := range c.Identities() {
		dedans := false
		for _, compte := range identite.Accounts {
			if _, membre := teams.Of(equipes, compte); membre {
				dedans = true
				break
			}
		}
		if dedans {
			continue
		}
		restants = append(restants, identite.Person())
	}
	return restants
}
