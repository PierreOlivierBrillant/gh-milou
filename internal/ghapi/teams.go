package ghapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/PierreOlivierBrillant/gh-milou/internal/teams"
)

// Les équipes d'organisation sont ce que GitHub Classroom utilisait pour les
// travaux d'équipe, et l'outil fait pareil : l'accès à un dépôt est accordé à
// l'équipe, pas à chacun de ses membres.
//
// Ces points d'API demandent la portée « admin:org » : lire les équipes se
// contente de « read:org », mais en créer, en renommer, en supprimer ou en
// changer la composition n'est possible qu'avec la première.

// ListTeamMembers renvoie les comptes inscrits dans une équipe, et ceux qui y
// ont été invités sans avoir encore accepté.
//
// Les seconds comptent autant que les premiers. Inscrire dans une équipe
// quelqu'un qui n'est pas encore membre de l'organisation ne l'y met pas : cela
// l'invite, et GitHub le laisse « en attente » jusqu'à ce qu'il accepte le
// courriel. Il ne figure alors pas parmi les membres, et l'équipe paraît
// n'avoir rien reçu — alors que tout s'est bien passé.
func (c *Client) ListTeamMembers(org, slug string) (members, pending []string, err error) {
	path := teamPath(org, slug) + "/members"
	err = c.paginate(path, nil, func(content []byte) (int, error) {
		var page []User
		if err := json.Unmarshal(content, &page); err != nil {
			return 0, err
		}
		for _, item := range page {
			members = append(members, item.Login)
		}
		return len(page), nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Une invitation illisible n'efface pas les membres qu'on a lus : elle
	// laisse seulement l'équipe paraître plus petite qu'elle n'est.
	deja := make(map[string]bool, len(members))
	for _, login := range members {
		deja[strings.ToLower(login)] = true
	}
	_ = c.paginate(teamPath(org, slug)+"/invitations", nil, func(content []byte) (int, error) {
		var page []User
		if err := json.Unmarshal(content, &page); err != nil {
			return 0, err
		}
		for _, item := range page {
			if item.Login == "" || deja[strings.ToLower(item.Login)] {
				continue
			}
			deja[strings.ToLower(item.Login)] = true
			pending = append(pending, item.Login)
		}
		return len(page), nil
	})
	return members, pending, nil
}

// CreateTeam crée une équipe dans l'organisation et renvoie ce qu'elle est
// devenue : GitHub tire le « slug » du nom, et c'est lui qui l'adresse ensuite.
func (c *Client) CreateTeam(org, name, description, privacy string) (*teams.Info, error) {
	body := map[string]any{
		"name":        name,
		"description": description,
		"privacy":     privacy,
	}
	response, err := c.do(http.MethodPost, "orgs/"+url.PathEscape(org)+"/teams", body)
	if err != nil {
		return nil, err
	}
	return readTeam(response)
}

// UpdateTeam renomme une équipe et met sa description à jour. Le « slug »
// change avec le nom : c'est celui que GitHub renvoie qui vaut ensuite.
func (c *Client) UpdateTeam(org, slug, name, description string) (*teams.Info, error) {
	body := map[string]any{"name": name, "description": description}
	response, err := c.do(http.MethodPatch, teamPath(org, slug), body)
	if err != nil {
		return nil, err
	}
	return readTeam(response)
}

// DeleteTeam supprime définitivement une équipe. Les dépôts qu'elle voyait
// restent ; c'est l'accès qui disparaît.
func (c *Client) DeleteTeam(org, slug string) error {
	_, err := c.do(http.MethodDelete, teamPath(org, slug), nil, http.StatusNotFound)
	return err
}

// AddTeamMember inscrit une personne dans l'équipe. Le rôle « member » ne lui
// donne aucun droit sur l'équipe elle-même.
func (c *Client) AddTeamMember(org, slug, username, role string) error {
	path := teamPath(org, slug) + "/memberships/" + url.PathEscape(username)
	_, err := c.do(http.MethodPut, path, map[string]any{"role": role})
	return err
}

// RemoveTeamMember retire une personne de l'équipe.
func (c *Client) RemoveTeamMember(org, slug, username string) error {
	path := teamPath(org, slug) + "/memberships/" + url.PathEscape(username)
	_, err := c.do(http.MethodDelete, path, nil, http.StatusNotFound)
	return err
}

func readTeam(response *Response) (*teams.Info, error) {
	value := &Team{}
	if err := response.JSON(value); err != nil {
		return nil, &Error{Message: "Équipe illisible : " + err.Error()}
	}
	return &teams.Info{
		Slug: value.Slug, Name: value.Name, Description: value.Description,
	}, nil
}

func teamPath(org, slug string) string {
	return "orgs/" + url.PathEscape(org) + "/teams/" + url.PathEscape(strings.TrimSpace(slug))
}

// TeamNames lit les équipes de l'organisation sans leur composition. C'est ce
// qu'il faut pour savoir quels travaux sont d'équipe — le nom suffit —, et cela
// tient en une requête par centaine d'équipes, là où leurs membres en coûtent
// deux par équipe.
func (c *Client) TeamNames(org string) ([]teams.Info, error) {
	listees, err := c.ListOrgTeams(org)
	if err != nil {
		return nil, err
	}
	found := make([]teams.Info, 0, len(listees))
	for _, item := range listees {
		found = append(found, teams.Info{
			Slug: item.Slug, Name: item.Name, Description: item.Description,
		})
	}
	return found, nil
}

// LoadOrgTeams lit les équipes de l'organisation et leur composition. Les
// membres se lisent une équipe à la fois : les requêtes partent en parallèle,
// car une organisation d'établissement peut en compter des centaines.
//
// Une équipe dont les membres restent illisibles n'arrête pas le tout : elle
// est rendue sans eux, et l'interface la montre vide plutôt que de refuser
// d'afficher quoi que ce soit.
func (c *Client) LoadOrgTeams(org string, jobs int) ([]teams.Info, error) {
	found, err := c.TeamNames(org)
	if err != nil {
		return nil, err
	}
	if jobs < 1 {
		jobs = 1
	}
	var wait sync.WaitGroup
	tickets := make(chan struct{}, jobs)
	for index := range found {
		wait.Add(1)
		tickets <- struct{}{}
		go func(position int) {
			defer wait.Done()
			defer func() { <-tickets }()
			members, pending, err := c.ListTeamMembers(org, found[position].Slug)
			if err != nil {
				return
			}
			found[position].Members = append(members, pending...)
			found[position].Pending = pending
		}(index)
	}
	wait.Wait()
	return found, nil
}

// ListRepoTeams renvoie les équipes qui ont accès à un dépôt.
//
// C'est par là qu'un travail d'équipe fait avec GitHub Classroom se relit : le
// dépôt n'y est pas partagé avec des collaborateurs, mais avec une équipe. Ses
// membres ne sont donc visibles nulle part ailleurs — la liste des
// collaborateurs directs revient vide, et le dépôt paraît n'appartenir à
// personne.
func (c *Client) ListRepoTeams(owner, repo string) ([]teams.Info, error) {
	var all []teams.Info
	err := c.paginate(repoPath(owner, repo)+"/teams", nil, func(content []byte) (int, error) {
		var page []Team
		if err := json.Unmarshal(content, &page); err != nil {
			return 0, err
		}
		for _, item := range page {
			all = append(all, teams.Info{
				Slug: item.Slug, Name: item.Name, Description: item.Description,
			})
		}
		return len(page), nil
	})
	return all, err
}

// ListContributors renvoie les comptes qui ont écrit dans un dépôt, du plus
// prolifique au moins prolifique.
//
// C'est le dernier recours pour savoir qui a fait un travail : ni accès ni
// équipe ne le disent toujours, mais ce qui a été poussé, si. Un dépôt sans
// aucun commit répond 204 ; ce n'est pas une erreur, seulement une réponse
// vide.
func (c *Client) ListContributors(owner, repo string) ([]string, error) {
	var all []string
	err := c.paginate(repoPath(owner, repo)+"/contributors", nil, func(content []byte) (int, error) {
		// Un dépôt sans aucun commit répond 204, sans corps : ce n'est pas une
		// réponse illisible, c'est une réponse vide.
		if len(bytes.TrimSpace(content)) == 0 {
			return 0, nil
		}
		var page []struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(content, &page); err != nil {
			return 0, err
		}
		for _, item := range page {
			// Les robots — « github-classroom[bot] », les actions — écrivent
			// eux aussi : ils ne font partie d'aucune équipe.
			if strings.EqualFold(item.Type, "Bot") || strings.Contains(item.Login, "[bot]") {
				continue
			}
			all = append(all, item.Login)
		}
		return len(page), nil
	})
	return all, err
}
