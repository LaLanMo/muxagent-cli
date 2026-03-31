package tasktui

import "charm.land/bubbles/v2/key"

type appKeyMap struct {
	quit            key.Binding
	open            key.Binding
	back            key.Binding
	confirm         key.Binding
	up              key.Binding
	down            key.Binding
	left            key.Binding
	right           key.Binding
	prevQuestion    key.Binding
	nextQuestion    key.Binding
	copy            key.Binding
	nextFocus       key.Binding
	toggleDetailTab key.Binding
	toggleWorktree  key.Binding
	prevConfig      key.Binding
	nextConfig      key.Binding
	renameConfig    key.Binding
	deleteConfig    key.Binding
}

func newAppKeyMap() appKeyMap {
	return appKeyMap{
		quit:            key.NewBinding(key.WithKeys("ctrl+c")),
		open:            key.NewBinding(key.WithKeys("enter")),
		back:            key.NewBinding(key.WithKeys("esc")),
		confirm:         key.NewBinding(key.WithKeys("enter")),
		up:              key.NewBinding(key.WithKeys("up")),
		down:            key.NewBinding(key.WithKeys("down")),
		left:            key.NewBinding(key.WithKeys("left")),
		right:           key.NewBinding(key.WithKeys("right")),
		prevQuestion:    key.NewBinding(key.WithKeys("ctrl+p")),
		nextQuestion:    key.NewBinding(key.WithKeys("ctrl+n")),
		copy:            key.NewBinding(key.WithKeys("c")),
		nextFocus:       key.NewBinding(key.WithKeys("tab")),
		toggleDetailTab: key.NewBinding(key.WithKeys("shift+tab")),
		toggleWorktree:  key.NewBinding(key.WithKeys("ctrl+t")),
		prevConfig:      key.NewBinding(key.WithKeys("ctrl+p")),
		nextConfig:      key.NewBinding(key.WithKeys("ctrl+n")),
		renameConfig:    key.NewBinding(key.WithKeys("r")),
		deleteConfig:    key.NewBinding(key.WithKeys("x")),
	}
}
