# temp python file, will replace this with go lang
import re

with open("internal/phases/launch.go", "r") as f:
    content = f.read()

# Functions/structs to remove
to_remove = [
    r"type LaunchContext struct\s*{[^}]*}",
    r"type LaunchResult struct\s*{[^}]*}",
    r"func RunLaunch\([^)]*\)[^{]*\{.*?\n}\n", # This is too complex for simple regex
]

# A better way is to find function start and end
# or just use a basic parser.

def remove_block(name, kind="func"):
    global content
    if kind == "func":
        pattern = re.compile(rf"func {name}\(.*?^{{.*?\n}}\n", re.MULTILINE | re.DOTALL)
    else:
        pattern = re.compile(rf"type {name} struct\s*{{.*?^{{.*?\n}}\n", re.MULTILINE | re.DOTALL) # wait, type is different
        
