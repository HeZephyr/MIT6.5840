#!/bin/bash

echo "go test -run 3A"
for i in {1..50}
do
  echo "Running test iteration $i"
  output=$(go test -run 3A 2>&1) # 2>&1 将stderr重定向到stdout
  if [[ $? -ne 0 ]]; then
    echo "Error in iteration $i:"
    echo "$output"
  fi
done
