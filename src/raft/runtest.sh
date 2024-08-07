#!/bin/bash

# check the number of arguments
if [ "$#" -ne 2 ]; then
  echo "Usage: $0 <test_type> <iterations>"
  echo "test_type must be one of 3A, 3B, 3C, 3D"
  exit 1
fi

test_type=$1
iterations=$2

# check the test_type
if [[ "$test_type" != "3A" && "$test_type" != "3B" && "$test_type" != "3C" && "$test_type" != "3D" ]]; then
  echo "Invalid test_type: $test_type"
  echo "test_type must be one of 3A, 3B, 3C, 3D"
  exit 1
fi

# check the iterations is a positive integer
if ! [[ "$iterations" =~ ^[0-9]+$ ]]; then
  echo "Invalid iterations: $iterations"
  echo "iterations must be a positive integer"
  exit 1
fi

echo "go test -run $test_type"
for ((i=1; i<=iterations; i++))
do
  echo "Running test iteration $i"
  output=$(go test -run $test_type 2>&1) # 2>&1 redirects stderr to stdout
  if [[ $? -ne 0 ]]; then
    echo "Error in iteration $i:"
    echo "$output"
  fi
done