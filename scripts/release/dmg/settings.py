# dmgbuild settings for the Block drag-to-install window.
# Invoked by scripts/release/package-release.mjs:
#   dmgbuild -s settings.py -D app=<Block.app> -D background=<tiff> Block <out.dmg>
import os.path

app = defines["app"]  # noqa: F821 - injected by dmgbuild
app_name = os.path.basename(app)

volume_name = "Block"
format = "UDZO"
filesystem = "HFS+"
files = [app]
symlinks = {"Applications": "/Applications"}
background = defines["background"]  # noqa: F821

show_status_bar = False
show_tab_view = False
show_toolbar = False
show_pathbar = False
show_sidebar = False
window_rect = ((200, 160), (600, 380))
default_view = "icon-view"
show_icon_preview = False
include_icon_view_settings = True
arrange_by = None
icon_size = 96
text_size = 12
label_pos = "bottom"
icon_locations = {app_name: (170, 168), "Applications": (430, 168)}
