package tasktui

import "charm.land/bubbles/v2/key"

type appKeyMap struct {
	quit         key.Binding
	open         key.Binding
	back         key.Binding
	confirm      key.Binding
	up           key.Binding
	down         key.Binding
	nextFocus    key.Binding
	tabTimeline  key.Binding
	tabArtifacts key.Binding
	prevConfig   key.Binding
	nextConfig   key.Binding
}

func newAppKeyMap() appKeyMap {
	return appKeyMap{
		quit:         key.NewBinding(key.WithKeys("ctrl+c")),
		open:         key.NewBinding(key.WithKeys("enter")),
		back:         key.NewBinding(key.WithKeys("esc")),
		confirm:      key.NewBinding(key.WithKeys("enter")),
		up:           key.NewBinding(key.WithKeys("up")),
		down:         key.NewBinding(key.WithKeys("down")),
		nextFocus:    key.NewBinding(key.WithKeys("tab")),
		tabTimeline:  key.NewBinding(key.WithKeys("1")),
		tabArtifacts: key.NewBinding(key.WithKeys("2")),
		prevConfig:   key.NewBinding(key.WithKeys("ctrl+p")),
		nextConfig:   key.NewBinding(key.WithKeys("ctrl+n")),
	}
}
