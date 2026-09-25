# Blender, used through its interface

Claude building a snowman in Blender by hand (mouse, menus, keystrokes)
while `movie desk` films it: the desktop route of the skill, on Linux.

```bash
docker build -t movie-blender examples/blender
examples/blender/start.sh
m=skills/proving-it-works-with-a-movie/bin/movie
$m desk start snow/ --display :99 --title "A snowman, built in Blender" -- docker exec blender-desk
$m desk shot snow/                      # look, then act, then look again
$m desk click snow/ 660 420
$m desk key snow/ x
$m desk click snow/ 633 420 --say "I select the cube and delete it."
...
$m desk stop snow/ snow-takes/
$m build snow-takes/scenes.yaml snowman.mp4
docker stop blender-desk
```

The Dockerfile saves Blender's preferences so no first-run splash covers
the viewport. Blender draws with Mesa's software OpenGL; Eevee renders,
slowly. Two things Blender taught `movie desk`: a click must hover a moment
and hold the button briefly or Blender ignores it, and a render takes long
enough to need `wait --quiet 2 --timeout 120`.
