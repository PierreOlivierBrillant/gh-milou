package web

import (
	"net/http"

	"github.com/PierreOlivierBrillant/gh-milou/internal/classroom"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/registry"
	"github.com/PierreOlivierBrillant/gh-milou/internal/roster"
)

// Un groupe se désigne par sa place — « a26.5n6.1010 » —, et cette place est
// écrite dans le nom de chacun de ses dépôts. Il n'y a donc rien à retrouver
// dans un fichier local pour l'ouvrir : la place suffit, et ce qu'on retient
// d'un groupe — sa liste, ses réglages — vient s'y greffer quand il existe.
//
// C'est ce qui fait qu'un groupe présent dans l'organisation s'affiche sans
// avoir été déclaré, et qu'un lien vers lui vaut d'une machine à l'autre.

// org est l'organisation de la session, celle que l'interface a choisie.
func (s *Server) org() string { return s.Settings().Org }

// place résout la place demandée : le groupe qu'on a déclaré là, ou, à défaut,
// celui que sa place seule décrit.
func (s *Server) place(request *http.Request) (classroom.Classroom, error) {
	return s.placeAt(request.PathValue("scope"))
}

// placeAt résout une place donnée autrement que par l'adresse — le groupe
// d'arrivée d'un déplacement, par exemple.
// Les noms sont versés ici, dès la résolution : le fichier local ne les garde
// plus une fois le registre à jour, et tout ce qui suit — afficher, planifier,
// refuser un travail faute de nom — les attend présents. Sans inventaire à
// disposition, seuls les noms des personnes déjà listées sont comblés ; les
// inscriptions que les dépôts révèlent viennent avec « enrichi ».
func (s *Server) placeAt(scope string) (classroom.Classroom, error) {
	org := s.org()
	set, _ := s.names(org)
	if cours, trouve := s.classrooms.Find(org, scope); trouve {
		return cours.Scheduling(set).Staffing(set).Enrich(set, nil), nil
	}
	cours, err := classroom.AtScope(org, scope, classroom.DefaultsFrom(s.Settings()))
	if err != nil {
		return cours, err
	}
	return cours.Scheduling(set).Staffing(set).Enrich(set, nil), nil
}

// enrichi verse dans un groupe ce que le registre de l'organisation sait de ses
// personnes. Il s'applique dès que l'inventaire est en main, avant tout ce qui
// lit ou planifie sur des identités.
//
// Ce qui en vient n'est pas écrit sur le disque : le magasin retire à
// l'enregistrement ce qui a été déduit plutôt que déclaré.
func (s *Server) enrichi(cours classroom.Classroom, repos []groups.RepoInfo) classroom.Classroom {
	set, _ := s.names(cours.Org)
	// Le registre répond aux trois questions qu'un groupe lui pose : qui se
	// cache derrière un nom de dépôt, quand chaque travail est attendu, et qui
	// enseigne — sans quoi un commit d'enseignant daterait la remise.
	return cours.Scheduling(set).Staffing(set).Enrich(set, repos)
}

// apprendre confie au registre de l'organisation ce qu'on vient d'apprendre
// des personnes : leur nom, et le slug que ce nom donnera à leurs dépôts.
//
// C'est fait avant d'écrire quoi que ce soit d'autre. Un registre qui refuse
// arrête donc l'opération au lieu de la laisser à moitié faite — et surtout,
// un nom qui n'aurait pas atteint le registre serait invisible pour tout le
// monde sauf cette machine, ce qui est exactement ce qu'on veut cesser.
//
// L'écriture est idempotente : redire au registre ce qu'il sait déjà n'y écrit
// rien, et le cas courant ne coûte donc qu'une lecture.
func (s *Server) apprendre(org string, people ...roster.Person) error {
	// Qui n'a pas de compte n'y entre pas (voir registry.Learn) : une liste
	// du collège entière ne coûte alors pas même la lecture.
	change := registry.Learn(people...)
	if change.Empty() {
		return nil
	}
	_, err := s.registryOf(org).Apply(change)
	return err
}

// visibles rassemble les groupes de l'organisation. Le magasin en décide : le
// terminal doit voir les mêmes groupes que le navigateur.
func (s *Server) visibles(org string, repos []groups.RepoInfo) []classroom.Classroom {
	set, _ := s.names(org)
	vus := s.classrooms.Visible(org, repos, classroom.DefaultsFrom(s.Settings()), set)
	for position, cours := range vus {
		vus[position] = cours.Scheduling(set)
	}
	return vus
}

// echeances traduit ce que le domaine veut faire des dates de remise en une
// écriture du registre. C'est le seul endroit où les deux vocabulaires se
// rencontrent : la notion de groupe décrit ses intentions, le registre les
// écrit, et ni l'un ni l'autre n'a besoin de connaître l'autre paquet.
func echeances(lignes []classroom.Deadline) registry.Change {
	travaux := make([]registry.Assignment, 0, len(lignes))
	for _, ligne := range lignes {
		travaux = append(travaux, registry.Assignment{ID: ligne.Assignment, Due: ligne.Due})
	}
	return registry.Reschedule(travaux...)
}
