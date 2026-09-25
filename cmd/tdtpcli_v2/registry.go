package main

// RegisterAll wires every v2 command into the App. One place, alphabetical.
func RegisterAll(a *App) {
	a.Register(newDiffCommand())
	a.Register(newExportBrokerCommand())
	a.Register(newExportCommand())
	a.Register(newExportXLSXCommand())
	a.Register(newFromXLSXCommand())
	a.Register(newImportBrokerCommand())
	a.Register(newImportCommand())
	a.Register(newImportXLSXCommand())
	a.Register(newInspectCommand())
	a.Register(newInspectTableCommand())
	a.Register(newListCommand())
	a.Register(newMergeCommand())
	a.Register(newPipelineCommand())
	a.Register(newSyncCommand())
	a.Register(newTestCommand())
	a.Register(newToCSVCommand())
	a.Register(newToCompactCommand())
	a.Register(newToHTMLCommand())
	a.Register(newToJSONCommand())
	a.Register(newToTDTPCommand())
	a.Register(newToXLSXCommand())
	a.Register(newValidateCommand())
}
