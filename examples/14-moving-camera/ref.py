from manim import *


class ChangePositionAndSizeCamera(MovingCameraScene):
    def construct(self):
        text = Text("nabla u", font_size=120)
        square = Square()

        VGroup(text, square).arrange(RIGHT, buff=3)
        self.add(text, square)

        self.camera.frame.save_state()

        self.play(
            self.camera.frame.animate.set(width=text.width * 1.2).move_to(text)
        )
        self.wait()

        self.play(Restore(self.camera.frame))

        self.play(
            self.camera.frame.animate.set(height=square.width * 1.2).move_to(square)
        )
        self.wait()

        self.play(Restore(self.camera.frame))
        self.wait()
