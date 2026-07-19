package main

import "fmt"

func main() {
	fmt.Println("=== schemix examples ===")

	fmt.Println("\n1. Basic validation + computed fields:")
	basicExample()

	fmt.Println("\n2. Nested objects + arrays:")
	nestedExample()

	fmt.Println("\n3. @meta field control:")
	metaExample()

	fmt.Println("\n4. FailMode comparison:")
	failModeExample()

	fmt.Println("\n5. Bloblang pipeline integration:")
	pipelineExample()

	fmt.Println("\n6. Registry management:")
	registryExample()

	fmt.Println("\n7. Error handling (chain API):")
	errorHandlingExample()

	fmt.Println("\n8. Construction options (MustNew/Context/FromValue):")
	convenienceExample()

	fmt.Println("\n9. Bloblang function-style invocation:")
	functionExample()

	fmt.Println("\n10. API request validation (production pattern):")
	apiValidationExample()

	fmt.Println("\n11. Custom error messages (i18n):")
	formatterExample()

	fmt.Println("\n12. Custom functions (4 styles):")
	customFuncExample()

	fmt.Println("\n13. FuncMap (reusable function collections):")
	funcMapExample()

	fmt.Println("\n14. Override built-in validators:")
	overrideExample()

	fmt.Println("\n15. Schema introspection:")
	introspectExample()

	fmt.Println("\n16. Result chain API:")
	resultChainExample()

	fmt.Println("\n17. Schema composition (NewFromValue):")
	compositionExample()

	fmt.Println("\n18. Built-in validation methods (37+):")
	builtinValidatorsExample()
}
