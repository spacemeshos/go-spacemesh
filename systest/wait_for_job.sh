#!/bin/bash
set -u
echo Waiting for job/$test_job_name to complete or fail
kubectl wait --for=condition=complete job/$test_job_name --timeout=-1s & complete_id=$!
kubectl wait --for=condition=failed job/$test_job_name --timeout=-1s && exit 1 & fail_id=$!
wait -n $complete_id $fail_id
exit_code=$?

if [[ $exit_code -eq 0 ]]; then
    kill $fail_id
else
    kill $complete_id
fi

exit $exit_code
