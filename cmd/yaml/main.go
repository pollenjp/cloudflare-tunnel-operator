package main

import (
	"log"

	yamlGoYaml "go.yaml.in/yaml/v4"
)

func main() {
	yamlContent := `
foo: FOO
bar: 1
items:
  - x: X
    y: Y
  - x: X
    y: Y
  - x: X
    y: Y
`

	log.Println("----------------------------------------")
	log.Println("yaml/go-yaml:")
	{
		var anyYaml map[string]interface{}
		if err := yamlGoYaml.Unmarshal([]byte(yamlContent), &anyYaml); err != nil {
			log.Fatal(err)
		}
		log.Println(anyYaml)

		// add new key value
		anyYaml["newKey"] = "newValue"

		// insert value to 'items' array
		items, ok := anyYaml["items"].([]interface{})
		if !ok {
			log.Fatal("'items' is not an array")
		}
		items = append(items, map[string]interface{}{
			"x": "X added",
			"y": "Y added",
		})
		anyYaml["items"] = items

		// Add new array
		key := "arr"
		anyYaml[key] = (func(key string) []interface{} {
			type arrType []interface{}
			arr, _ := anyYaml[key].(arrType)

			// add an item
			arr = append(arr, map[string]interface{}{
				"x": "X added",
				"y": "Y added",
			})
			return arr
		})(key)

		// marshal to yaml
		yamlBytes, err := yamlGoYaml.Marshal(anyYaml)
		if err != nil {
			log.Fatal(err)
		}
		log.Println(string(yamlBytes))
	}
}
