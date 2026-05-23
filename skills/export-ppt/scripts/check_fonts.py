import matplotlib.font_manager as fm
fonts = [f.name for f in fm.fontManager.ttflist if any(kw in f.name.lower() for kw in ['yahei','simhei','hei','song','microsoft'])]
print("Found Chinese fonts:", list(set(fonts)))
