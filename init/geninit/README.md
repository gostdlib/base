# gen

`gen` is used to create a company wide `init` package that ties back to the `init` package defined here.

You can then add custom registrations to that package for registrations specific to your companies needs that are not included here.

To do this, you simply use RegisterInit/RegisterClose inside an `func init()` you define. Those will happen before any custom registrations for a package are added and are applied company wide.

Standard registrions can still be done project by project.
