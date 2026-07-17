package main

import "fmt"

func main() {
	fmt.Println("=== schemix examples ===")

	fmt.Println("\n1. Basic validation + value extraction:")
	basicExample()

	fmt.Println("\n2. Nested objects + arrays:")
	nestedExample()

	fmt.Println("\n3. @meta field control:")
	metaExample()

	fmt.Println("\n4. FailMode comparison:")
	failModeExample()

	fmt.Println("\n5. Bloblang pipeline:")
	pipelineExample()

	fmt.Println("\n6. Registry management:")
	registryExample()

	fmt.Println("\n7. Error handling:")
	errorHandlingExample()

	fmt.Println("\n8. MustNew + shared Context:")
	convenienceExample()

	fmt.Println("\n9. Function-style invocation:")
	functionExample()

	fmt.Println("\n10. API request validation:")
	apiValidationExample()

	fmt.Println("\n11. Custom error messages (i18n):")
	formatterExample()

	fmt.Println("\n12. Custom validation functions:")
	customFuncExample()

	fmt.Println("\n13. Schema introspection:")
	introspectExample()

	fmt.Println("\n14. Result chain API:")
	resultChainExample()

	fmt.Println("\n15. Schema composition (NewFromValue):")
	compositionExample()

	fmt.Println("\n16. Built-in validation methods:")
	builtinValidatorsExample()
}
