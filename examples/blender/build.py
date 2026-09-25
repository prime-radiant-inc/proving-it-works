# Builds a snowman in Blender's running UI, one visible step at a time, and
# logs when each step happens to /work/steps.jsonl, so film.sh can cut the
# screen capture into one narrated scene per step. Run it with
#   blender --python build.py
import json
import math
import time

import bpy
from mathutils import Euler

LOG = "/work/steps.jsonl"
PAUSE = 2.5  # seconds each step stays on screen before the next


def log(step):
    with open(LOG, "a") as f:
        f.write(json.dumps({"step": step, "t": time.time()}) + "\n")


def material(name, rgb):
    m = bpy.data.materials.get(name) or bpy.data.materials.new(name)
    m.diffuse_color = (*rgb, 1)
    return m


def add(kind, name, location, colour, **size):
    add_op = {"sphere": bpy.ops.mesh.primitive_uv_sphere_add,
              "cone": bpy.ops.mesh.primitive_cone_add,
              "cylinder": bpy.ops.mesh.primitive_cylinder_add}[kind]
    add_op(location=location, **size)
    obj = bpy.context.active_object
    obj.name = name
    obj.data.materials.append(colour)
    if kind == "sphere":
        bpy.ops.object.shade_smooth()
    return obj


def view():
    for area in bpy.context.screen.areas:
        if area.type == "VIEW_3D":
            return area.spaces.active


def look(rotation_z):
    r3d = view().region_3d
    r3d.view_location = (0, 0, 2.3)
    r3d.view_distance = 7.5
    r3d.view_rotation = Euler((math.radians(75), 0, math.radians(rotation_z))).to_quaternion()


white = material("snow", (0.95, 0.95, 1.0))
orange = material("carrot", (1.0, 0.45, 0.05))
coal = material("coal", (0.03, 0.03, 0.03))


def start():
    space = view()
    space.shading.type = "SOLID"
    space.shading.color_type = "MATERIAL"
    space.overlay.show_floor = True
    look(30)
    log("start")


def body():
    for name in ("Cube", "Camera", "Light"):
        bpy.data.objects.remove(bpy.data.objects[name])
    add("sphere", "Body", (0, 0, 1.0), white, radius=1.0, segments=48, ring_count=24)
    log("body")


def middle_and_head():
    add("sphere", "Middle", (0, 0, 2.35), white, radius=0.72, segments=48, ring_count=24)
    add("sphere", "Head", (0, 0, 3.4), white, radius=0.48, segments=48, ring_count=24)
    log("middle_and_head")


def nose():
    add("cone", "Nose", (0, -0.62, 3.4), orange, radius1=0.09, radius2=0, depth=0.45,
        rotation=(math.radians(90), 0, 0))
    log("nose")


def coal_bits():
    for x in (-0.16, 0.16):
        add("sphere", "Eye", (x, -0.42, 3.55), coal, radius=0.06)
    # buttons sit on the middle sphere's surface (radius 0.72, centre 2.35)
    for z in (2.1, 2.35, 2.6):
        add("sphere", "Button", (0, -math.sqrt(0.72 ** 2 - (z - 2.35) ** 2), z), coal, radius=0.07)
    log("coal")


def hat():
    add("cylinder", "Brim", (0, 0, 3.82), coal, radius=0.55, depth=0.06)
    add("cylinder", "Top", (0, 0, 4.1), coal, radius=0.34, depth=0.55)
    bpy.ops.object.select_all(action="DESELECT")
    log("hat")


def orbit(done=[0]):
    # one full turn around the snowman in six seconds
    done[0] += 1
    look(30 + done[0] * 3)
    if done[0] == 1:
        log("orbit")
    if done[0] < 120:
        return 0.05
    log("end")
    bpy.app.timers.register(quit, first_interval=PAUSE)
    return None


def quit():
    bpy.ops.wm.quit_blender()


def run(steps):
    def tick():
        steps.pop(0)()
        if steps:
            return PAUSE
        bpy.app.timers.register(orbit, first_interval=PAUSE)
        return None
    return tick


# Give the window time to draw before the first step.
bpy.app.timers.register(run([start, body, middle_and_head, nose, coal_bits, hat]), first_interval=3.0)
