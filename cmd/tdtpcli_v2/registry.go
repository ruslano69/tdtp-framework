package main

// RegisterAll wires every v2 command into the App. One place, alphabetical.
func RegisterAll(a *App) {
	a.Register(newExportCommand())
	a.Register(newImportCommand())
	a.Register(newInspectCommand())
	a.Register(newListCommand())
	a.Register(newPipelineCommand())
	a.Register(newTestCommand())
	a.Register(newToCSVCommand())
	a.Register(newToCompactCommand())
	a.Register(newToHTMLCommand())
	a.Register(newToTDTPCommand())
	a.Register(newToXLSXCommand())
	a.Register(newValidateCommand())
}
