# A Linux desktop with Blender for the skill's desktop-capture route: Xvfb
# is the display (Mesa's llvmpipe gives Blender its OpenGL), ffmpeg films it.
FROM debian:trixie

RUN apt-get update && apt-get install -y --no-install-recommends \
      blender xvfb xdotool ffmpeg libgl1-mesa-dri python3 procps

ENV DISPLAY=:99
WORKDIR /work
CMD ["sleep", "infinity"]

# Saved preferences mean no first-run splash over the viewport.
RUN blender -b --factory-startup --python-expr \
      "import bpy; bpy.context.preferences.view.show_splash = False; bpy.ops.wm.save_userpref()"
