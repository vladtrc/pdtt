from manim import *


class Old2(Scene):
    def construct(self):
        box = Rectangle(
            height=4, width=6, fill_color=BLUE, fill_opacity=0.55, stroke_color=WHITE
        )
        text = always_redraw(lambda: Text("ln(2)", font_size=42).move_to(box.get_center()))

        self.add(box, text)
        self.play(box.animate.scale(0.5).to_edge(UL), run_time=3)
        self.wait()

        text.clear_updaters()

        self.play(box.animate.to_edge(RIGHT), run_time=3)
        self.wait()
