package model

type ClearOptions struct {
	DryRun            bool
	OnlyFailed        bool
	IncludeDownstream bool
	IncludeUpstream   bool
}
