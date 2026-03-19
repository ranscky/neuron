package runtime

// Runtime defines the interface for runtime execution
type Runtime interface {
	// Run executes the entry file with the given arguments and environment variables
	Run(entry string, args []string, env map[string]string) error
	
	// Name returns the name of the runtime (e.g., "python", "node")
	Name() string
}