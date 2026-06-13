from manim import *
import numpy as np


class ConstructPolynomialWithGivenRoots(Scene):
    def construct(self):
        poly = Text("P(x)=x^3+c2x^2+c1x+c0", font_size=42).to_corner(UL)
        question = Text(
            "Can you construct a cubic with roots at x=1, x=2, x=4?",
            font_size=30,
        ).next_to(poly, DOWN, buff=0.45, aligned_edge=LEFT)
        self.play(Write(poly), FadeIn(question, shift=DOWN))
        self.wait()

        axes = Axes(
            x_range=[-4, 4, 1],
            y_range=[-6, 6, 2],
            x_length=7.0,
            y_length=5.0,
        )
        graph = axes.plot(lambda x: (x - 1) * (x - 2) * (x - 4), color=BLUE)
        roots = [1, 2, 4]
        root_dots = VGroup(*[Dot(axes.c2p(x, 0), color=YELLOW) for x in roots])

        self.play(FadeIn(axes))
        self.play(Create(graph), run_time=3, rate_func=linear)
        self.play(LaggedStart(*[FadeIn(dot, scale=0.6) for dot in root_dots], lag_ratio=0.2))
        self.wait()

        factored = Text("P(x)=(x-r1)(x-r2)(x-r3)", font_size=40).next_to(poly, DOWN, buff=0.35)
        self.play(FadeOut(question), FadeIn(factored, shift=DOWN))

        arrows = VGroup(
            *[
                Arrow(
                    start=factored.get_bottom() + RIGHT * (i - 1) * 1.2,
                    end=dot.get_top(),
                    buff=0.15,
                    color=YELLOW,
                )
                for i, dot in enumerate(root_dots)
            ]
        )
        self.play(LaggedStart(*[Create(arrow) for arrow in arrows], lag_ratio=0.12))
        self.wait()

        self.play(
            poly.animate.set_opacity(0.25),
            axes.animate.set_opacity(0.35),
            graph.animate.set_color(WHITE),
            FadeOut(arrows),
            factored.animate.move_to(ORIGIN).scale(1.2),
            run_time=2,
        )
        self.wait(2)
