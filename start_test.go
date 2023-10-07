package task_test

import (
	"context"
	"fmt"
	"time"

	"github.com/kardianos/task"
)

func ExampleStart() {
	run := func(ctx context.Context) error {
		return fmt.Errorf("Return errors at top level")
	}
	err := task.Start(context.Background(), time.Second*2, run)
	if err != nil {
		fmt.Println(err)
	}

	// Output:
	// Return errors at top level
}
