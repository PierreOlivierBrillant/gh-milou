package web

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/clone"
	"github.com/PierreOlivierBrillant/gh-milou/internal/groups"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// handleFindClones liste les dépôts git présents sous un dossier.
func (s *Server) handleFindClones(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Directory string `json:"directory"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	found, err := clone.FindClones(body.Directory)
	if err != nil {
		fail(writer, err)
		return
	}
	if len(found) == 0 {
		fail(writer, valid.Errorf("Aucun dépôt git directement sous « %s ».", body.Directory))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"directory": body.Directory, "clones": found,
	})
}

// handleClone récupère tout ou partie d'un groupe dans un dossier local.
func (s *Server) handleClone(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Org         string   `json:"org"`
		Names       []string `json:"names"`
		Destination string   `json:"destination"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	org, err := valid.Login(body.Org, "Organisation")
	if err != nil {
		fail(writer, err)
		return
	}
	if len(body.Names) == 0 {
		fail(writer, valid.Errorf("Aucun dépôt sélectionné."))
		return
	}
	if _, err := clone.EnsureGit(); err != nil {
		fail(writer, err)
		return
	}

	// Les dépôts sont retrouvés dans l'inventaire par leur nom : un dépôt qui
	// n'y figure pas n'est pas de cette organisation.
	repos, _, err := s.repos(org, false)
	if err != nil {
		fail(writer, err)
		return
	}
	connus := map[string]groups.RepoInfo{}
	for _, repo := range repos {
		connus[strings.ToLower(repo.Name)] = repo
	}
	targets := make([]clone.Target, 0, len(body.Names))
	for _, name := range body.Names {
		repo, trouve := connus[strings.ToLower(name)]
		if !trouve {
			fail(writer, valid.Errorf("« %s » n'existe pas dans « %s ».", name, org))
			return
		}
		targets = append(targets, clone.Target{
			Name: repo.Name,
			URL: s.urlOf(org, groups.Repo{
				Name: repo.Name, URL: repo.HTMLURL, Private: repo.Private,
			}),
		})
	}

	destination, err := clone.PrepareDestination(body.Destination)
	if err != nil {
		fail(writer, err)
		return
	}
	s.rememberCloneDir(destination)

	jobs, depth := s.deps.Jobs, s.deps.Depth
	job := s.jobs.Start("clonage", valid.Plural(len(targets), "dépôt")+" vers "+destination,
		func(job *Job) (any, error) {
			results, err := clone.New(jobs, depth).Run(targets, destination,
				func(done, total int, result clone.Result) {
					job.Progress(done, total, result.Name)
					job.Line(result.Name+" : "+result.Status, result)
				})
			if err != nil {
				return nil, err
			}
			return cloneSummary(results, destination), nil
		})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// handlePull met à jour des clones existants, sans jamais écraser un travail local.
func (s *Server) handlePull(writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Directory string   `json:"directory"`
		Names     []string `json:"names"`
	}
	if err := decode(request, &body); err != nil {
		fail(writer, err)
		return
	}
	if _, err := clone.EnsureGit(); err != nil {
		fail(writer, err)
		return
	}
	found, err := clone.FindClones(body.Directory)
	if err != nil {
		fail(writer, err)
		return
	}
	wanted := map[string]bool{}
	for _, name := range body.Names {
		wanted[strings.ToLower(name)] = true
	}
	chosen := make([]clone.Clone, 0, len(found))
	for _, item := range found {
		if len(wanted) == 0 || wanted[strings.ToLower(item.Name)] {
			chosen = append(chosen, item)
		}
	}
	if len(chosen) == 0 {
		fail(writer, valid.Errorf("Aucun clone sélectionné."))
		return
	}
	if absolute, err := filepath.Abs(body.Directory); err == nil {
		s.rememberCloneDir(absolute)
	}

	jobs := s.deps.Jobs
	job := s.jobs.Start("pull", "Mise à jour de "+valid.Plural(len(chosen), "clone"),
		func(job *Job) (any, error) {
			results, err := clone.New(jobs, 0).Update(chosen,
				func(done, total int, result clone.Result) {
					job.Progress(done, total, result.Name)
					job.Line(result.Name+" : "+result.Status, result)
				})
			if err != nil {
				return nil, err
			}
			return cloneSummary(results, body.Directory), nil
		})
	writeJSON(writer, http.StatusAccepted, job.State())
}

// cloneSummary compte les issues d'un lot de clonages.
func cloneSummary(results []clone.Result, destination string) map[string]any {
	counts := map[string]int{}
	for _, result := range results {
		counts[result.Status]++
	}
	return map[string]any{
		"destination": destination,
		"results":     results,
		"cloned":      counts[clone.Cloned],
		"updated":     counts[clone.Updated],
		"skipped":     counts[clone.Skipped],
		"failed":      counts[clone.Failed],
	}
}

// rememberCloneDir retient le dossier parent des clones pour la prochaine fois.
func (s *Server) rememberCloneDir(destination string) {
	s.mutex.Lock()
	s.settings.CloneDir = filepath.Dir(destination)
	s.mutex.Unlock()
}
