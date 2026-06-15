package harness

// type MemoryStore interface {
// 	Save(key string, value string) error
// 	Load(key string) (string, error)
// }

// type Toolbox interface {
// 	List() ([]string, error)
// 	Execute(name string, arguments any) (string, error)
// }

// type ExecutionContext struct {
// 	Store MemoryStore
// 	Tools Toolbox
// }

type Harness struct {
	agent Agent
	// exctx *ExecutionContext
}

func New(agent Agent) (*Harness, error) {
	return &Harness{
		agent: agent,
		// exctx: &ExecutionContext{
		// 	Store: store,
		// 	Tools: tools,
		// },
	}, nil
}
