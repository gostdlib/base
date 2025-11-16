This is a fork and change of github.com/tidwall/btree/.

This changes the btree to remove non-generic items, maps and sets.
It also changes Set() so if the item already exists, it returns that item instead of replacing it.
The return values are changed to reflect this.

I've included the original license here.
