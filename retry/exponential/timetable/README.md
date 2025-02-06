# Timetable tool

## Introduction

Using exponential retries with custom policies requires understanding what the custom policy will do. This tool is provided to allow you to see the progression of time the exponential retry package will take.

There are a few caveats to pay attention to.

- None of this counts the time it takes for the underlying call that is being retried
- If a permanent error happens, this time is cut short of course

## Using timetable

You can simply modify the settings you want to see inside `settings.hujson`. It should be set to the default settings. Please do not check in changes to the file.

Once you've added your custom policy settings, you can simply `go run .` in this directory to get the output.

If you want to restrict it to some number of attempts, you can use the `-attempts` flag. It defaults to -1, which outputs the table until you reach your max interval.

If you want to output the data as a Go struct representation of a TimeTable, you can use `-gostruct`. This is really only useful for internal testing.
