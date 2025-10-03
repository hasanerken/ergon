#!/bin/bash

# Fix all test files - remove double ergon prefixes and stray braces
find test -name "*.go" -type f | while read f; do
  # Remove double ergon prefixes
  sed -i.bak \
    -e 's/ergon\.Addergon\./ergon.Add/g' \
    -e 's/ergon\.Newergon\./ergon.New/g' \
    -e 's/Assertergon\./Assert/g' \
    -e 's/ergon\.ergon\./ergon./g' \
    -e 's/Waitergon\./Wait/g' \
    -e 's/Createergon\./Create/g' \
    "$f" && rm -f "$f.bak"
done

# Fix specific stray braces in badger_test.go
sed -i.bak '/store := helper.NewBadgerStore()/{ n; /^$/{ n; /^\t}$/d; } }' test/integration/badger_test.go && rm -f test/integration/badger_test.go.bak

