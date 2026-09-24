package main

// RegisterAll wires every v2 command into the App. One place, alphabetical.
func RegisterAll(a *App) {
	a.Register(newInspectCommand())
	a.Register(newTestCommand())
	a.Register(newValidateCommand())
}
