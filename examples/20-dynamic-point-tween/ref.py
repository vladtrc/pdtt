from manim import *
import numpy as np


class DynamicPointTween(Scene):
    def construct(self):
        theta = ValueTracker(0.0)
        source = Dot(point=np.array([-3.0, 0.0, 0.0]), radius=0.14, color=YELLOW)
        target = always_redraw(
            lambda: Dot(
                point=np.array(
                    [
                        2.8 * np.cos(theta.get_value()),
                        2.2 * np.sin(theta.get_value()),
                        0.0,
                    ]
                ),
                radius=0.12,
                color=RED,
            )
        )
        self.add(source, target)
        start = source.get_center().copy()
        self.play(
            theta.animate.set_value(TAU),
            UpdateFromAlphaFunc(
                source,
                lambda mob, alpha: mob.move_to(
                    interpolate(start, target.get_center(), alpha)
                ).set_color(interpolate_color(YELLOW, RED, alpha)),
            ),
            run_time=4,
            rate_func=linear,
        )
        source.add_updater(lambda mob: mob.move_to(target.get_center()))
        self.play(theta.animate.set_value(1.5 * TAU), run_time=2, rate_func=linear)
