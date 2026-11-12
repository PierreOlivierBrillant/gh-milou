package app

import (
	"context"
	"os"
	"strings"

	"github.com/PierreOlivierBrillant/gh-milou/internal/scopes"
	"github.com/PierreOlivierBrillant/gh-milou/internal/ui"
	"github.com/PierreOlivierBrillant/gh-milou/internal/valid"
)

// grantedScopes énumère ce que le jeton annonce, et dit si on le sait : un
// jeton « fine-grained » n'annonce rien du tout.
func (s *Session) grantedScopes() ([]string, bool) {
	if s.Client == nil {
		return nil, false
	}
	return s.Client.ScopeList()
}

// tokenHost nomme l'hôte auquel le jeton se rapporte.
func (s *Session) tokenHost() string {
	if s.Client != nil {
		return s.Client.Host()
	}
	if s.Options.Host != "" {
		return s.Options.Host
	}
	return ""
}

// refreshToken redemande à gh un jeton portant les portées voulues, puis le
// fait suivre au client de la session : la suite de la séance en profite sans
// qu'il faille relancer l'outil.
func (s *Session) refreshToken(wanted, remove []string) error {
	refresher := s.Refresher
	if refresher == nil {
		refresher = scopes.NewRefresher()
	}
	s.Console.Note("gh demande confirmation dans le navigateur ; le code à recopier " +
		"paraît ci-dessous.")
	s.Console.Blank()

	token, err := refresher.Do(context.Background(), scopes.Request{
		Host:   s.tokenHost(),
		Origin: s.tokenOrigin,
		Add:    wanted,
		Remove: remove,
		In:     os.Stdin,
		Out:    s.Console.Out,
		Err:    s.Console.Out,
	})
	if err != nil {
		return err
	}
	s.tokenOrigin = "gh"
	if s.Client == nil {
		s.Console.Success("Jeton renouvelé.")
		return nil
	}
	if err := s.Client.UseToken(token); err != nil {
		return err
	}
	// C'est cet appel qui révèle les portées du nouveau jeton : sans lui, elles
	// resteraient inconnues jusqu'à la première requête utile.
	if _, err := s.Client.AuthenticatedUser(); err != nil {
		return err
	}
	granted, _ := s.grantedScopes()
	if absent := scopes.Missing(granted, wanted); len(absent) > 0 {
		s.Console.Warning("%s : %s. GitHub n'accorde que ce qui lui a été accordé dans le "+
			"navigateur.", valid.Agree(len(absent), "Toujours absente", "Toujours absentes"),
			strings.Join(absent, ", "))
		return nil
	}
	s.Console.Success("Jeton renouvelé.")
	return nil
}

// askedScopes rassemble ce qu'il faut demander : tout ce que le jeton porte
// déjà, plus ce qui manque. Obtenir un droit de plus ne doit jamais en faire
// perdre un autre.
func (s *Session) askedScopes(extra ...string) []string {
	granted, _ := s.grantedScopes()
	minimal := make([]string, 0, len(scopes.Catalog))
	for _, scope := range scopes.Catalog {
		if scope.Minimal {
			minimal = append(minimal, scope.Name)
		}
	}
	return scopes.Union(scopes.Union(granted, minimal), extra)
}

// ensureScope prévient qu'une portée manque et propose de la demander tout de
// suite. Il rend vrai quand l'action peut continuer.
func (s *Session) ensureScope(scope, consequence string) bool {
	present, known := false, false
	if s.Client != nil {
		present, known = s.Client.HasScope(scope)
	}
	if !known || present {
		return true
	}
	s.Console.Warning("Le jeton n'a pas la portée « %s » : %s.", scope, consequence)
	return s.offerScope(scope)
}

// offerScope propose de renouveler le jeton pour obtenir une portée absente. Il
// rend vrai quand elle est acquise, et que l'action a donc lieu d'être reprise.
func (s *Session) offerScope(scope string) bool {
	if scopes.FromEnvironment(s.tokenOrigin) {
		s.Console.Note("Ce jeton vient de la variable %s : gh ne peut pas le renouveler.",
			s.tokenOrigin)
		return false
	}
	wanted := s.askedScopes(scope)
	if !s.Interactive() {
		s.Console.Note("Pour l'obtenir : %s", scopes.Command(s.tokenHost(), wanted, nil))
		return false
	}

	if described, found := scopes.Find(scope); found {
		s.Console.Note("%s — %s", described.Label, described.Purpose)
	}
	confirmed, err := s.Prompt.Confirm("Générer un nouveau jeton avec cette portée ?", true)
	if err != nil || !confirmed {
		return false
	}
	if err := s.refreshToken(wanted, nil); err != nil {
		s.Console.Failure("%v", err)
		return false
	}
	// Reprendre l'action sans la portée voulue ne ferait que répéter le refus ;
	// un jeton qui n'annonce rien, lui, ne permet plus de trancher.
	granted, known := s.grantedScopes()
	return !known || scopes.Has(granted, scope)
}

// manageScopes est l'écran des portées : ce que le jeton permet, et ce qu'il
// permettrait. C'est le pendant, au terminal, de la boîte « Portées du jeton »
// des réglages généraux de l'interface web.
func (s *Session) manageScopes() error {
	// Parler du jeton suppose de l'avoir lu : les options avancées, elles,
	// s'ouvrent avant toute authentification.
	if s.Client == nil {
		if err := s.authenticate(); err != nil {
			s.Console.Failure("%v", err)
			return nil
		}
	}

	s.Console.Heading("Portées du jeton")
	granted, known := s.grantedScopes()
	inventory := scopes.Inventory(granted, known)
	rows := make([][]string, 0, len(inventory))
	for _, portee := range inventory {
		rows = append(rows, []string{portee.Name, portee.State, portee.Purpose})
	}
	s.Console.Table([]string{"Portée", "État", "Ce qu'elle permet"}, rows, 12)
	if !known {
		s.Console.Note("Ce jeton n'annonce aucune portée : c'en est un « fine-grained », " +
			"et ses droits se règlent sur GitHub.")
	}
	if scopes.FromEnvironment(s.tokenOrigin) {
		s.Console.Warning("Ce jeton vient de la variable %s : gh ne peut pas le renouveler. "+
			"Effacez-la et lancez « gh auth login », ou donnez-lui un jeton portant les "+
			"portées voulues.", s.tokenOrigin)
		return nil
	}
	if !s.Interactive() {
		return nil
	}

	options := make([]ui.Option, 0, len(inventory))
	selected := make([]bool, 0, len(inventory))
	for _, portee := range inventory {
		options = append(options, ui.Option{
			Value: portee.Name,
			Label: portee.Name + " — " + portee.Purpose,
		})
		// Le socle de gh accompagne tout jeton qu'il crée : le décocher n'aurait
		// aucun effet, sinon faire échouer la commande.
		selected = append(selected, portee.Minimal || portee.State == scopes.Present)
	}
	chosen, err := s.Prompt.MultiSelect("Portées voulues", options, selected)
	if err != nil {
		return err
	}

	var wanted []string
	for _, index := range chosen {
		wanted = append(wanted, inventory[index].Name)
	}
	add := s.askedScopes(wanted...)
	var remove []string
	for _, portee := range inventory {
		if !portee.Minimal && portee.State == scopes.Present && !scopes.Has(wanted, portee.Name) {
			remove = append(remove, portee.Name)
		}
	}
	if len(remove) == 0 && len(scopes.Missing(granted, wanted)) == 0 {
		s.Console.Note("Le jeton porte déjà ces portées : rien à faire.")
		return nil
	}

	s.Console.Note("Commande : %s", scopes.Command(s.tokenHost(), add, remove))
	confirmed, err := s.Prompt.Confirm("Générer un nouveau jeton ?", true)
	if err != nil || !confirmed {
		s.Console.Warning("Annulé : le jeton est inchangé.")
		return err
	}
	if err := s.refreshToken(add, remove); err != nil {
		s.Console.Failure("%v", err)
	}
	return nil
}

// refreshTokenFromFlags exécute « --refresh-token » : les portées demandées en
// ligne de commande s'ajoutent à celles déjà acquises, et rien n'est retiré.
func (s *Session) refreshTokenFromFlags() error {
	extra, err := parseScopes(s.Options.Scopes)
	if err != nil {
		return err
	}
	// Le jeton courant peut n'avoir aucun droit utile : son refus ne doit pas
	// empêcher d'en demander un meilleur.
	if err := s.authenticate(); err != nil {
		s.Console.Note("Jeton actuel inutilisable (%v) : le renouvellement continue.", err)
	}
	s.Console.Heading("Portées du jeton")
	wanted := s.askedScopes(extra...)
	s.Console.Printf("  Demandées : %s", s.Console.Bold(strings.Join(wanted, ", ")))
	return s.refreshToken(wanted, nil)
}

// parseScopes lit la valeur de « --scopes » : une liste séparée par des
// virgules. Vide, elle vaut pour toutes les portées dont l'outil se sert.
func parseScopes(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		names := make([]string, 0, len(scopes.Catalog))
		for _, scope := range scopes.Catalog {
			names = append(names, scope.Name)
		}
		return names, nil
	}
	var list []string
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) == "" {
			continue
		}
		name, err := scopes.Validate(item)
		if err != nil {
			return nil, err
		}
		list = append(list, name)
	}
	if len(list) == 0 {
		return nil, valid.Errorf("Portées : « %s » ne nomme aucune portée.", value)
	}
	return list, nil
}
