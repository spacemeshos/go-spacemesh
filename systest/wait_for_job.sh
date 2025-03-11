#!/bin/bash
set -u

# Function to clean up background processes
cleanup() {
    # Kill both background processes if they exist
    if [[ -n "${complete_id:-}" ]]; then
        kill $complete_id 2>/dev/null || true
    fi
    if [[ -n "${fail_id:-}" ]]; then
        kill $fail_id 2>/dev/null || true
    fi

    # Make sure there are no kubectl wait processes left for this job
    pkill -f "kubectl wait --for=condition=.*job/$test_job_name" 2>/dev/null || true

    exit ${1:-$exit_code}
}

# Set up trap to ensure cleanup happens on exit
trap 'cleanup' EXIT INT TERM

echo Waiting for job/$test_job_name to complete or fail

# Start the wait processes
kubectl wait --for=condition=complete job/$test_job_name --timeout=-1s & complete_id=$!
kubectl wait --for=condition=failed job/$test_job_name --timeout=-1s  && exit 1 & fail_id=$!

# Wait for either process to finish
wait -n $complete_id $fail_id
exit_code=$?

