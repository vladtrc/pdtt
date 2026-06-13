from manim import *
import numpy as np


class Show_Coords_of_MovingPoint(Scene):
    def construct(self):
        plane = NumberPlane()
        self.play(Create(plane))

        p = Dot(RIGHT * 3, color=GREEN)
        coords = always_redraw(
            lambda: Text(
                f"({p.get_center()[0]:.2f}, {p.get_center()[1]:.2f})",
                font_size=32,
                color=GREEN,
            ).next_to(p, DOWN, buff=0.2)
        )

        self.play(Create(p))
        self.wait(0.5)
        self.play(FadeIn(coords))
        self.wait()
        self.play(Rotating(p, angle=TAU, about_point=ORIGIN), run_time=10)
        self.wait(4)
