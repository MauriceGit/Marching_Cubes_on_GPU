package main

import (
    . "GPUTerrain/Geometry"
    . "GPUTerrain/Camera"
    . "GPUTerrain/OpenGL"
    "runtime"
    "github.com/go-gl/mathgl/mgl32"
    "fmt"
    "C"
    "unsafe"
    "github.com/go-gl/gl/v4.5-core/gl"
    "github.com/go-gl/glfw/v3.2/glfw"
    //"github.com/MauriceGit/half"
)

// Constants and global variables

const (
    g_WindowWidth  = 1000
    g_WindowHeight = 1000

    g_cubeWidth    = 10
    g_cubeHeight   = 10
    g_cubeDepth    = 10

)

const g_WindowTitle  = "First test to create Marching Cubes"
var g_ShaderID uint32


// Normal Camera
var g_fovy      = mgl32.DegToRad(90.0)
var g_aspect    = float32(g_WindowWidth)/g_WindowHeight
var g_nearPlane = float32(0.1)
var g_farPlane  = float32(2000.0)

var g_viewMatrix          mgl32.Mat4

var g_light Object


type Vertex struct {
    Pos      mgl32.Vec4
    Normal   mgl32.Vec4
}

type Triangle struct {
    Vertices []Vertex
}

// One "unit" consist of i.e. 10^3 small cubes for which triangles
// are calculated using the MarchingCubes algorithm.
// One unit is dispatched all at once to the GPU and calculated in parallel.
// Several units allow for more/larger areas to be triangulated.
type MarchingCubeUnit struct {
    PositionOffset      mgl32.Vec3
    LocalWorkGroupCount int
    RenderTriangleCount int
    // Shows the outline (as wireframe) of a Marching-Cube-Unit (Box/Cube)
    BoxOutline          Object
    ShowOutline         bool
}

// This holds all the information, counters and buffers
// to create and render the marching cubes, consisting of several
// "units" (blocks that are dispatched to the GPU consecutively).
type MarchingCubes struct {
    // The shader used for calculating the marching cubes.
    ShaderID                uint32
    // This is the worst case, if every cube actually creates 5 triangles.
    // This is not realistic, so optimize later!
    TriangleCount           int
    // This arraybuffer is is main handle to the calculated positions on the GPU.
    PositionArrayBuffer     uint32
    // This points to the vertex buffer (see g_positionBuffer) for rendering
    PositionVertexBuffer    uint32
    // The buffer, the first run of marching cubes writes the triangle count into, they like to generate.
    TriangleLayoutSizesBuffer   uint32
    // How many units we create.
    UnitCount               int
    // The instances of every Marching cube
    MarchingCubeUnits       []MarchingCubeUnit

}

var g_marchingCubes MarchingCubes


var g_timeSum float32 = 0.0
var g_lastCallTime float64 = 0.0
var g_frameCount int = 0
var g_fps float32 = 60.0

var g_fillMode = 0

func init() {
    // GLFW event handling must run on the main OS thread
    runtime.LockOSThread()
}


func printHelp() {
    fmt.Println(
        `Help yourself.`,
    )
}

// Set OpenGL version, profile and compatibility
func initGraphicContext() (*glfw.Window, error) {
    glfw.WindowHint(glfw.Resizable, glfw.True)
    glfw.WindowHint(glfw.ContextVersionMajor, 4)
    glfw.WindowHint(glfw.ContextVersionMinor, 3)
    glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
    glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)

    window, err := glfw.CreateWindow(g_WindowWidth, g_WindowHeight, g_WindowTitle, nil, nil)
    if err != nil {
        return nil, err
    }
    window.MakeContextCurrent()

    // Initialize Glow
    if err := gl.Init(); err != nil {
        return nil, err
    }

    return window, nil
}

func defineModelMatrix(shader uint32, pos, scale mgl32.Vec3) {
    matScale := mgl32.Scale3D(scale.X(), scale.Y(), scale.Z())
    matTrans := mgl32.Translate3D(pos.X(), pos.Y(), pos.Z())
    model := matTrans.Mul4(matScale)
    modelUniform := gl.GetUniformLocation(shader, gl.Str("modelMat\x00"))
    gl.UniformMatrix4fv(modelUniform, 1, false, &model[0])
}

// Defines the Model-View-Projection matrices for the shader.
func defineMatrices(shader uint32) {
    projection := mgl32.Perspective(g_fovy, g_aspect, g_nearPlane, g_farPlane)
    camera := mgl32.LookAtV(GetCameraLookAt())

    viewProjection := projection.Mul4(camera);
    cameraUniform := gl.GetUniformLocation(shader, gl.Str("viewProjectionMat\x00"))
    gl.UniformMatrix4fv(cameraUniform, 1, false, &viewProjection[0])
}

func renderObject(shader uint32, obj Object) {

    // Model transformations are now encoded per object directly before rendering it!
    defineModelMatrix(shader, obj.Pos, obj.Scale)

    gl.BindVertexArray(obj.Geo.VertexObject)

    gl.Uniform3fv(gl.GetUniformLocation(shader, gl.Str("color\x00")), 1, &obj.Color[0])
    gl.Uniform3fv(gl.GetUniformLocation(shader, gl.Str("light\x00")), 1, &g_light.Pos[0])
    var isLighti int32 = 0
    if obj.IsLight {
        isLighti = 1
    }
    gl.Uniform1i(gl.GetUniformLocation(shader, gl.Str("isLight\x00")), isLighti)

    gl.DrawArrays(gl.TRIANGLES, 0, obj.Geo.VertexCount)

    gl.BindVertexArray(0)

}

func renderPositionBuffer(shader uint32) {

    defineModelMatrix(shader, mgl32.Vec3{0,0,0}, mgl32.Vec3{1,1,1})

    color := mgl32.Vec3{1,0,0}
    gl.Uniform3fv(gl.GetUniformLocation(shader, gl.Str("color\x00")), 1, &color[0])
    gl.Uniform3fv(gl.GetUniformLocation(shader, gl.Str("light\x00")), 1, &g_light.Pos[0])
    var isLighti int32 = 0
    gl.Uniform1i(gl.GetUniformLocation(shader, gl.Str("isLight\x00")), isLighti)

    /* Vertex-Buffer zum Rendern der Positionen */
    gl.BindVertexArray (g_marchingCubes.PositionVertexBuffer);
    gl.DrawArrays(gl.TRIANGLES, 0, int32(3*g_marchingCubes.TriangleCount));
    gl.BindVertexArray(0)
}


func renderEverything(shader uint32) {

    gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
    gl.Enable(gl.DEPTH_TEST)
    // Nice blueish background
    gl.ClearColor(135.0/255.,206.0/255.,235.0/255., 1.0)

    gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)
    gl.Viewport(0, 0, g_WindowWidth, g_WindowHeight)

    gl.UseProgram(shader)

    defineMatrices(shader)

    renderObject(shader, g_light)

    renderPositionBuffer(shader)

    var polyMode int32
    gl.GetIntegerv(gl.POLYGON_MODE, &polyMode)
    gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)
    for i,_ := range g_marchingCubes.MarchingCubeUnits {
        renderObject(shader, g_marchingCubes.MarchingCubeUnits[i].BoxOutline)
    }
    gl.PolygonMode(gl.FRONT_AND_BACK, uint32(polyMode))

    gl.UseProgram(0)

}

func calculateAndRenderMarchingCubes(window *glfw.Window) {

    gl.UseProgram(g_marchingCubes.ShaderID)

    // This will fill the buffer with the sizes, that we need memory for in the next run.
    gl.Uniform1i(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("calculateSizeOnly\x00")), 1)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 3, g_marchingCubes.TriangleLayoutSizesBuffer);

    cubeUnitSize := g_cubeWidth * g_cubeHeight * g_cubeDepth

    for i:=0; i < g_marchingCubes.UnitCount; i++ {
        gl.Uniform1i(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("cubeIndexOffset\x00")), int32(i * cubeUnitSize))
        gl.Uniform3fv(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("cubePositionOffset\x00")),1, &g_marchingCubes.MarchingCubeUnits[i].PositionOffset[0])
        gl.DispatchCompute(1, 1, 1)
    }

    gl.MemoryBarrier(gl.BUFFER_UPDATE_BARRIER_BIT)

    layoutArraySize := g_marchingCubes.UnitCount * cubeUnitSize

    // Add up all values, to determine the exact storage layout locations for each shader invocation
    ptr := gl.MapBufferRange(gl.SHADER_STORAGE_BUFFER, 0, layoutArraySize*int(unsafe.Sizeof(int32(0))), gl.MAP_WRITE_BIT | gl.MAP_READ_BIT)
    // This is really tricky. We get a pure C-Array pointer from glMapBufferRange, which is in our user address space.
    // Unfortunately, we cannot use it directly in Go or convert it easily to a usable slice.
    // So instead, we apply some magic (bit-shift-stuff) and work on C.int datatype directly. This should operate then directly
    // on the underlaying C-Array in memory.
    layoutSizes := (*[1 << 30]C.int)(unsafe.Pointer(ptr))[:layoutArraySize:layoutArraySize]
    var lastSize int = int(layoutSizes[layoutArraySize-1])
    var sum  C.int = 0
    var sum2 C.int = layoutSizes[0]
    for i := 1; i < layoutArraySize; i++ {
        sum = layoutSizes[i]
        layoutSizes[i] = sum2
        sum2 += sum
    }
    layoutSizes[0] = 0

    // This determines, how many triangles actually have to be rendered!
    lastTriangleCount := g_marchingCubes.TriangleCount
    g_marchingCubes.TriangleCount = int(layoutSizes[layoutArraySize-1]) + lastSize

    if lastTriangleCount != g_marchingCubes.TriangleCount {
        fmt.Println("triangle count: ", g_marchingCubes.TriangleCount)
        fmt.Println("triangles/cube: ", float32(g_marchingCubes.TriangleCount)/float32(layoutArraySize))
        fmt.Println("unit count:     ", g_marchingCubes.UnitCount)
    }

    gl.UnmapBuffer(gl.SHADER_STORAGE_BUFFER)

    // This will actually create the triangle data seamless in the g_positionBuffer.
    gl.Uniform1i(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("calculateSizeOnly\x00")), 0)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 2, g_marchingCubes.PositionArrayBuffer);

    for i:=0; i < g_marchingCubes.UnitCount; i++ {
        gl.Uniform1i(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("cubeIndexOffset\x00")), int32(i * cubeUnitSize))
        gl.Uniform3fv(gl.GetUniformLocation(g_marchingCubes.ShaderID, gl.Str("cubePositionOffset\x00")),1, &g_marchingCubes.MarchingCubeUnits[i].PositionOffset[0])
        gl.DispatchCompute(1, 1, 1)
    }


    gl.MemoryBarrier(gl.BUFFER_UPDATE_BARRIER_BIT)
    gl.UseProgram(0)


    renderEverything(g_ShaderID)

}

// Callback method for a keyboard press
func cbKeyboard(window *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {

    // All changes come VERY easy now.
    if action == glfw.Press {
        switch key {
            // Close the Simulation.
            case glfw.KeyEscape, glfw.KeyQ:
                window.SetShouldClose(true)
            case glfw.KeyH:
                printHelp()
            case glfw.KeySpace:
            case glfw.KeyF1:
                g_fillMode += 1
                switch (g_fillMode%3) {
                    case 0:
                        gl.PolygonMode(gl.FRONT_AND_BACK, gl.FILL)
                    case 1:
                        gl.PolygonMode(gl.FRONT_AND_BACK, gl.LINE)
                    case 2:
                        gl.PolygonMode(gl.FRONT_AND_BACK, gl.POINT)
                }
            case glfw.KeyF2:

            case glfw.KeyF3:
            case glfw.KeyUp:
                g_light.Pos = g_light.Pos.Add(mgl32.Vec3{0,1.0,0})
            case glfw.KeyDown:
                g_light.Pos = g_light.Pos.Add(mgl32.Vec3{0,-1.0,0})
            case glfw.KeyLeft:
            case glfw.KeyRight:
        }
    }

}

// see: https://github.com/go-gl/glfw/blob/master/v3.2/glfw/input.go
func cbMouseScroll(window *glfw.Window, xpos, ypos float64) {
    UpdateMouseScroll(xpos, ypos)
}

func cbMouseButton(window *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
    UpdateMouseButton(button, action, mods)
}

func cbCursorPos(window *glfw.Window, xpos, ypos float64) {
    UpdateCursorPos(xpos, ypos)
}


// Register all needed callbacks
func registerCallBacks (window *glfw.Window) {
    window.SetKeyCallback(cbKeyboard)
    window.SetScrollCallback(cbMouseScroll)
    window.SetMouseButtonCallback(cbMouseButton)
    window.SetCursorPosCallback(cbCursorPos)
}


func displayFPS(window *glfw.Window) {
    currentTime := glfw.GetTime()
    g_timeSum += float32(currentTime - g_lastCallTime)


    if g_frameCount%60 == 0 {
        g_fps = float32(1.0) / (g_timeSum/60.0)
        g_timeSum = 0.0

        s := fmt.Sprintf("FPS: %.1f", g_fps)
        window.SetTitle(s)
    }

    g_lastCallTime = currentTime
    g_frameCount += 1

}

// Mainloop for graphics updates and object animation
func mainLoop (window *glfw.Window) {

    registerCallBacks(window)
    glfw.SwapInterval(0)

    for !window.ShouldClose() {

        displayFPS(window)

        // This actually renders everything.
        calculateAndRenderMarchingCubes(window)

        window.SwapBuffers()
        glfw.PollEvents()
    }



}

func createMarchingCubeConstBuffers(marchingCubesShaderID uint32) {

    caseToNumPolys := []int32{0, 1, 1, 2, 1, 2, 2, 3,  1, 2, 2, 3, 2, 3, 3, 2,  1, 2, 2, 3, 2, 3, 3, 4,  2, 3, 3, 4, 3, 4, 4, 3,
                               1, 2, 2, 3, 2, 3, 3, 4,  2, 3, 3, 4, 3, 4, 4, 3,  2, 3, 3, 2, 3, 4, 4, 3,  3, 4, 4, 3, 4, 5, 5, 2,
                               1, 2, 2, 3, 2, 3, 3, 4,  2, 3, 3, 4, 3, 4, 4, 3,  2, 3, 3, 4, 3, 4, 4, 5,  3, 4, 4, 5, 4, 5, 5, 4,
                               2, 3, 3, 4, 3, 4, 2, 3,  3, 4, 4, 5, 4, 5, 3, 2,  3, 4, 4, 3, 4, 5, 3, 2,  4, 5, 5, 4, 5, 2, 4, 1,
                               1, 2, 2, 3, 2, 3, 3, 4,  2, 3, 3, 4, 3, 4, 4, 3,  2, 3, 3, 4, 3, 4, 4, 5,  3, 2, 4, 3, 4, 3, 5, 2,
                               2, 3, 3, 4, 3, 4, 4, 5,  3, 4, 4, 5, 4, 5, 5, 4,  3, 4, 4, 3, 4, 5, 5, 4,  4, 3, 5, 2, 5, 4, 2, 1,
                               2, 3, 3, 4, 3, 4, 4, 5,  3, 4, 4, 5, 2, 3, 3, 2,  3, 4, 4, 5, 4, 5, 5, 2,  4, 3, 5, 4, 3, 2, 4, 1,
                               3, 4, 4, 5, 4, 5, 3, 4,  4, 5, 5, 2, 3, 4, 2, 1,  2, 3, 3, 2, 3, 4, 2, 1,  3, 2, 4, 1, 2, 1, 1, 0}

    var caseABO uint32 = 0
    gl.GenBuffers    (1, &caseABO);
    gl.BindBuffer    (gl.ARRAY_BUFFER, caseABO);
    gl.BufferData    (gl.ARRAY_BUFFER, len(caseToNumPolys)*int(unsafe.Sizeof(int32(0))), gl.Ptr(caseToNumPolys), gl.STATIC_READ);

    gl.UseProgram(marchingCubesShaderID)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 0, caseABO);
    gl.UseProgram(0)

    // List of 256 * 5 * vec3()
    //edgeConnectList := [][]int32{{-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,8,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,1,9,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,8,3,9,8,1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,2,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,8,3,1,2,10,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,2,10,0,2,9,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {2,8,3,2,10,8,10,9,8,-1,-1,-1,-1,-1,-1}, {3,11,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,11,2,8,11,0,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,9,0,2,3,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,11,2,1,9,11,9,8,11,-1,-1,-1,-1,-1,-1}, {3,10,1,11,10,3,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,10,1,0,8,10,8,11,10,-1,-1,-1,-1,-1,-1}, {3,9,0,3,11,9,11,10,9,-1,-1,-1,-1,-1,-1}, {9,8,10,10,8,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,7,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,3,0,7,3,4,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,1,9,8,4,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,1,9,4,7,1,7,3,1,-1,-1,-1,-1,-1,-1}, {1,2,10,8,4,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,4,7,3,0,4,1,2,10,-1,-1,-1,-1,-1,-1}, {9,2,10,9,0,2,8,4,7,-1,-1,-1,-1,-1,-1}, {2,10,9,2,9,7,2,7,3,7,9,4,-1,-1,-1}, {8,4,7,3,11,2,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {11,4,7,11,2,4,2,0,4,-1,-1,-1,-1,-1,-1}, {9,0,1,8,4,7,2,3,11,-1,-1,-1,-1,-1,-1}, {4,7,11,9,4,11,9,11,2,9,2,1,-1,-1,-1}, {3,10,1,3,11,10,7,8,4,-1,-1,-1,-1,-1,-1}, {1,11,10,1,4,11,1,0,4,7,11,4,-1,-1,-1}, {4,7,8,9,0,11,9,11,10,11,0,3,-1,-1,-1}, {4,7,11,4,11,9,9,11,10,-1,-1,-1,-1,-1,-1}, {9,5,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,5,4,0,8,3,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,5,4,1,5,0,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {8,5,4,8,3,5,3,1,5,-1,-1,-1,-1,-1,-1}, {1,2,10,9,5,4,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,0,8,1,2,10,4,9,5,-1,-1,-1,-1,-1,-1}, {5,2,10,5,4,2,4,0,2,-1,-1,-1,-1,-1,-1}, {2,10,5,3,2,5,3,5,4,3,4,8,-1,-1,-1}, {9,5,4,2,3,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,11,2,0,8,11,4,9,5,-1,-1,-1,-1,-1,-1}, {0,5,4,0,1,5,2,3,11,-1,-1,-1,-1,-1,-1}, {2,1,5,2,5,8,2,8,11,4,8,5,-1,-1,-1}, {10,3,11,10,1,3,9,5,4,-1,-1,-1,-1,-1,-1}, {4,9,5,0,8,1,8,10,1,8,11,10,-1,-1,-1}, {5,4,0,5,0,11,5,11,10,11,0,3,-1,-1,-1}, {5,4,8,5,8,10,10,8,11,-1,-1,-1,-1,-1,-1}, {9,7,8,5,7,9,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,3,0,9,5,3,5,7,3,-1,-1,-1,-1,-1,-1}, {0,7,8,0,1,7,1,5,7,-1,-1,-1,-1,-1,-1}, {1,5,3,3,5,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,7,8,9,5,7,10,1,2,-1,-1,-1,-1,-1,-1}, {10,1,2,9,5,0,5,3,0,5,7,3,-1,-1,-1}, {8,0,2,8,2,5,8,5,7,10,5,2,-1,-1,-1}, {2,10,5,2,5,3,3,5,7,-1,-1,-1,-1,-1,-1}, {7,9,5,7,8,9,3,11,2,-1,-1,-1,-1,-1,-1}, {9,5,7,9,7,2,9,2,0,2,7,11,-1,-1,-1}, {2,3,11,0,1,8,1,7,8,1,5,7,-1,-1,-1}, {11,2,1,11,1,7,7,1,5,-1,-1,-1,-1,-1,-1}, {9,5,8,8,5,7,10,1,3,10,3,11,-1,-1,-1}, {5,7,0,5,0,9,7,11,0,1,0,10,11,10,0}, {11,10,0,11,0,3,10,5,0,8,0,7,5,7,0}, {11,10,5,7,11,5,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {10,6,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,8,3,5,10,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,0,1,5,10,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,8,3,1,9,8,5,10,6,-1,-1,-1,-1,-1,-1}, {1,6,5,2,6,1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,6,5,1,2,6,3,0,8,-1,-1,-1,-1,-1,-1}, {9,6,5,9,0,6,0,2,6,-1,-1,-1,-1,-1,-1}, {5,9,8,5,8,2,5,2,6,3,2,8,-1,-1,-1}, {2,3,11,10,6,5,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {11,0,8,11,2,0,10,6,5,-1,-1,-1,-1,-1,-1}, {0,1,9,2,3,11,5,10,6,-1,-1,-1,-1,-1,-1}, {5,10,6,1,9,2,9,11,2,9,8,11,-1,-1,-1}, {6,3,11,6,5,3,5,1,3,-1,-1,-1,-1,-1,-1}, {0,8,11,0,11,5,0,5,1,5,11,6,-1,-1,-1}, {3,11,6,0,3,6,0,6,5,0,5,9,-1,-1,-1}, {6,5,9,6,9,11,11,9,8,-1,-1,-1,-1,-1,-1}, {5,10,6,4,7,8,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,3,0,4,7,3,6,5,10,-1,-1,-1,-1,-1,-1}, {1,9,0,5,10,6,8,4,7,-1,-1,-1,-1,-1,-1}, {10,6,5,1,9,7,1,7,3,7,9,4,-1,-1,-1}, {6,1,2,6,5,1,4,7,8,-1,-1,-1,-1,-1,-1}, {1,2,5,5,2,6,3,0,4,3,4,7,-1,-1,-1}, {8,4,7,9,0,5,0,6,5,0,2,6,-1,-1,-1}, {7,3,9,7,9,4,3,2,9,5,9,6,2,6,9}, {3,11,2,7,8,4,10,6,5,-1,-1,-1,-1,-1,-1}, {5,10,6,4,7,2,4,2,0,2,7,11,-1,-1,-1}, {0,1,9,4,7,8,2,3,11,5,10,6,-1,-1,-1}, {9,2,1,9,11,2,9,4,11,7,11,4,5,10,6}, {8,4,7,3,11,5,3,5,1,5,11,6,-1,-1,-1}, {5,1,11,5,11,6,1,0,11,7,11,4,0,4,11}, {0,5,9,0,6,5,0,3,6,11,6,3,8,4,7}, {6,5,9,6,9,11,4,7,9,7,11,9,-1,-1,-1}, {10,4,9,6,4,10,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,10,6,4,9,10,0,8,3,-1,-1,-1,-1,-1,-1}, {10,0,1,10,6,0,6,4,0,-1,-1,-1,-1,-1,-1}, {8,3,1,8,1,6,8,6,4,6,1,10,-1,-1,-1}, {1,4,9,1,2,4,2,6,4,-1,-1,-1,-1,-1,-1}, {3,0,8,1,2,9,2,4,9,2,6,4,-1,-1,-1}, {0,2,4,4,2,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {8,3,2,8,2,4,4,2,6,-1,-1,-1,-1,-1,-1}, {10,4,9,10,6,4,11,2,3,-1,-1,-1,-1,-1,-1}, {0,8,2,2,8,11,4,9,10,4,10,6,-1,-1,-1}, {3,11,2,0,1,6,0,6,4,6,1,10,-1,-1,-1}, {6,4,1,6,1,10,4,8,1,2,1,11,8,11,1}, {9,6,4,9,3,6,9,1,3,11,6,3,-1,-1,-1}, {8,11,1,8,1,0,11,6,1,9,1,4,6,4,1}, {3,11,6,3,6,0,0,6,4,-1,-1,-1,-1,-1,-1}, {6,4,8,11,6,8,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {7,10,6,7,8,10,8,9,10,-1,-1,-1,-1,-1,-1}, {0,7,3,0,10,7,0,9,10,6,7,10,-1,-1,-1}, {10,6,7,1,10,7,1,7,8,1,8,0,-1,-1,-1}, {10,6,7,10,7,1,1,7,3,-1,-1,-1,-1,-1,-1}, {1,2,6,1,6,8,1,8,9,8,6,7,-1,-1,-1}, {2,6,9,2,9,1,6,7,9,0,9,3,7,3,9}, {7,8,0,7,0,6,6,0,2,-1,-1,-1,-1,-1,-1}, {7,3,2,6,7,2,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {2,3,11,10,6,8,10,8,9,8,6,7,-1,-1,-1}, {2,0,7,2,7,11,0,9,7,6,7,10,9,10,7}, {1,8,0,1,7,8,1,10,7,6,7,10,2,3,11}, {11,2,1,11,1,7,10,6,1,6,7,1,-1,-1,-1}, {8,9,6,8,6,7,9,1,6,11,6,3,1,3,6}, {0,9,1,11,6,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {7,8,0,7,0,6,3,11,0,11,6,0,-1,-1,-1}, {7,11,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {7,6,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,0,8,11,7,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,1,9,11,7,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {8,1,9,8,3,1,11,7,6,-1,-1,-1,-1,-1,-1}, {10,1,2,6,11,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,2,10,3,0,8,6,11,7,-1,-1,-1,-1,-1,-1}, {2,9,0,2,10,9,6,11,7,-1,-1,-1,-1,-1,-1}, {6,11,7,2,10,3,10,8,3,10,9,8,-1,-1,-1}, {7,2,3,6,2,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {7,0,8,7,6,0,6,2,0,-1,-1,-1,-1,-1,-1}, {2,7,6,2,3,7,0,1,9,-1,-1,-1,-1,-1,-1}, {1,6,2,1,8,6,1,9,8,8,7,6,-1,-1,-1}, {10,7,6,10,1,7,1,3,7,-1,-1,-1,-1,-1,-1}, {10,7,6,1,7,10,1,8,7,1,0,8,-1,-1,-1}, {0,3,7,0,7,10,0,10,9,6,10,7,-1,-1,-1}, {7,6,10,7,10,8,8,10,9,-1,-1,-1,-1,-1,-1}, {6,8,4,11,8,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,6,11,3,0,6,0,4,6,-1,-1,-1,-1,-1,-1}, {8,6,11,8,4,6,9,0,1,-1,-1,-1,-1,-1,-1}, {9,4,6,9,6,3,9,3,1,11,3,6,-1,-1,-1}, {6,8,4,6,11,8,2,10,1,-1,-1,-1,-1,-1,-1}, {1,2,10,3,0,11,0,6,11,0,4,6,-1,-1,-1}, {4,11,8,4,6,11,0,2,9,2,10,9,-1,-1,-1}, {10,9,3,10,3,2,9,4,3,11,3,6,4,6,3}, {8,2,3,8,4,2,4,6,2,-1,-1,-1,-1,-1,-1}, {0,4,2,4,6,2,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,9,0,2,3,4,2,4,6,4,3,8,-1,-1,-1}, {1,9,4,1,4,2,2,4,6,-1,-1,-1,-1,-1,-1}, {8,1,3,8,6,1,8,4,6,6,10,1,-1,-1,-1}, {10,1,0,10,0,6,6,0,4,-1,-1,-1,-1,-1,-1}, {4,6,3,4,3,8,6,10,3,0,3,9,10,9,3}, {10,9,4,6,10,4,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,9,5,7,6,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,8,3,4,9,5,11,7,6,-1,-1,-1,-1,-1,-1}, {5,0,1,5,4,0,7,6,11,-1,-1,-1,-1,-1,-1}, {11,7,6,8,3,4,3,5,4,3,1,5,-1,-1,-1}, {9,5,4,10,1,2,7,6,11,-1,-1,-1,-1,-1,-1}, {6,11,7,1,2,10,0,8,3,4,9,5,-1,-1,-1}, {7,6,11,5,4,10,4,2,10,4,0,2,-1,-1,-1}, {3,4,8,3,5,4,3,2,5,10,5,2,11,7,6}, {7,2,3,7,6,2,5,4,9,-1,-1,-1,-1,-1,-1}, {9,5,4,0,8,6,0,6,2,6,8,7,-1,-1,-1}, {3,6,2,3,7,6,1,5,0,5,4,0,-1,-1,-1}, {6,2,8,6,8,7,2,1,8,4,8,5,1,5,8}, {9,5,4,10,1,6,1,7,6,1,3,7,-1,-1,-1}, {1,6,10,1,7,6,1,0,7,8,7,0,9,5,4}, {4,0,10,4,10,5,0,3,10,6,10,7,3,7,10}, {7,6,10,7,10,8,5,4,10,4,8,10,-1,-1,-1}, {6,9,5,6,11,9,11,8,9,-1,-1,-1,-1,-1,-1}, {3,6,11,0,6,3,0,5,6,0,9,5,-1,-1,-1}, {0,11,8,0,5,11,0,1,5,5,6,11,-1,-1,-1}, {6,11,3,6,3,5,5,3,1,-1,-1,-1,-1,-1,-1}, {1,2,10,9,5,11,9,11,8,11,5,6,-1,-1,-1}, {0,11,3,0,6,11,0,9,6,5,6,9,1,2,10}, {11,8,5,11,5,6,8,0,5,10,5,2,0,2,5}, {6,11,3,6,3,5,2,10,3,10,5,3,-1,-1,-1}, {5,8,9,5,2,8,5,6,2,3,8,2,-1,-1,-1}, {9,5,6,9,6,0,0,6,2,-1,-1,-1,-1,-1,-1}, {1,5,8,1,8,0,5,6,8,3,8,2,6,2,8}, {1,5,6,2,1,6,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,3,6,1,6,10,3,8,6,5,6,9,8,9,6}, {10,1,0,10,0,6,9,5,0,5,6,0,-1,-1,-1}, {0,3,8,5,6,10,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {10,5,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {11,5,10,7,5,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {11,5,10,11,7,5,8,3,0,-1,-1,-1,-1,-1,-1}, {5,11,7,5,10,11,1,9,0,-1,-1,-1,-1,-1,-1}, {10,7,5,10,11,7,9,8,1,8,3,1,-1,-1,-1}, {11,1,2,11,7,1,7,5,1,-1,-1,-1,-1,-1,-1}, {0,8,3,1,2,7,1,7,5,7,2,11,-1,-1,-1}, {9,7,5,9,2,7,9,0,2,2,11,7,-1,-1,-1}, {7,5,2,7,2,11,5,9,2,3,2,8,9,8,2}, {2,5,10,2,3,5,3,7,5,-1,-1,-1,-1,-1,-1}, {8,2,0,8,5,2,8,7,5,10,2,5,-1,-1,-1}, {9,0,1,5,10,3,5,3,7,3,10,2,-1,-1,-1}, {9,8,2,9,2,1,8,7,2,10,2,5,7,5,2}, {1,3,5,3,7,5,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,8,7,0,7,1,1,7,5,-1,-1,-1,-1,-1,-1}, {9,0,3,9,3,5,5,3,7,-1,-1,-1,-1,-1,-1}, {9,8,7,5,9,7,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {5,8,4,5,10,8,10,11,8,-1,-1,-1,-1,-1,-1}, {5,0,4,5,11,0,5,10,11,11,3,0,-1,-1,-1}, {0,1,9,8,4,10,8,10,11,10,4,5,-1,-1,-1}, {10,11,4,10,4,5,11,3,4,9,4,1,3,1,4}, {2,5,1,2,8,5,2,11,8,4,5,8,-1,-1,-1}, {0,4,11,0,11,3,4,5,11,2,11,1,5,1,11}, {0,2,5,0,5,9,2,11,5,4,5,8,11,8,5}, {9,4,5,2,11,3,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {2,5,10,3,5,2,3,4,5,3,8,4,-1,-1,-1}, {5,10,2,5,2,4,4,2,0,-1,-1,-1,-1,-1,-1}, {3,10,2,3,5,10,3,8,5,4,5,8,0,1,9}, {5,10,2,5,2,4,1,9,2,9,4,2,-1,-1,-1}, {8,4,5,8,5,3,3,5,1,-1,-1,-1,-1,-1,-1}, {0,4,5,1,0,5,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {8,4,5,8,5,3,9,0,5,0,3,5,-1,-1,-1}, {9,4,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,11,7,4,9,11,9,10,11,-1,-1,-1,-1,-1,-1}, {0,8,3,4,9,7,9,11,7,9,10,11,-1,-1,-1}, {1,10,11,1,11,4,1,4,0,7,4,11,-1,-1,-1}, {3,1,4,3,4,8,1,10,4,7,4,11,10,11,4}, {4,11,7,9,11,4,9,2,11,9,1,2,-1,-1,-1}, {9,7,4,9,11,7,9,1,11,2,11,1,0,8,3}, {11,7,4,11,4,2,2,4,0,-1,-1,-1,-1,-1,-1}, {11,7,4,11,4,2,8,3,4,3,2,4,-1,-1,-1}, {2,9,10,2,7,9,2,3,7,7,4,9,-1,-1,-1}, {9,10,7,9,7,4,10,2,7,8,7,0,2,0,7}, {3,7,10,3,10,2,7,4,10,1,10,0,4,0,10}, {1,10,2,8,7,4,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,9,1,4,1,7,7,1,3,-1,-1,-1,-1,-1,-1}, {4,9,1,4,1,7,0,8,1,8,7,1,-1,-1,-1}, {4,0,3,7,4,3,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {4,8,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {9,10,8,10,11,8,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,0,9,3,9,11,11,9,10,-1,-1,-1,-1,-1,-1}, {0,1,10,0,10,8,8,10,11,-1,-1,-1,-1,-1,-1}, {3,1,10,11,3,10,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,2,11,1,11,9,9,11,8,-1,-1,-1,-1,-1,-1}, {3,0,9,3,9,11,1,2,9,2,11,9,-1,-1,-1}, {0,2,11,8,0,11,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {3,2,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {2,3,8,2,8,10,10,8,9,-1,-1,-1,-1,-1,-1}, {9,10,2,0,9,2,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {2,3,8,2,8,10,0,1,8,1,10,8,-1,-1,-1}, {1,10,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {1,3,8,9,1,8,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,9,1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {0,3,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}, {-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}}
    edgeConnectList := []int32{-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,8,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,1,9,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,8,3,9,8,1,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,2,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,8,3,1,2,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,2,10,0,2,9,-1,-1,-1,-1,-1,-1,-1,-1,-1,2,8,3,2,10,8,10,9,8,-1,-1,-1,-1,-1,-1,3,11,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,11,2,8,11,0,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,9,0,2,3,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,11,2,1,9,11,9,8,11,-1,-1,-1,-1,-1,-1,3,10,1,11,10,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,10,1,0,8,10,8,11,10,-1,-1,-1,-1,-1,-1,3,9,0,3,11,9,11,10,9,-1,-1,-1,-1,-1,-1,9,8,10,10,8,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,7,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,3,0,7,3,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,1,9,8,4,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,1,9,4,7,1,7,3,1,-1,-1,-1,-1,-1,-1,1,2,10,8,4,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,4,7,3,0,4,1,2,10,-1,-1,-1,-1,-1,-1,9,2,10,9,0,2,8,4,7,-1,-1,-1,-1,-1,-1,2,10,9,2,9,7,2,7,3,7,9,4,-1,-1,-1,8,4,7,3,11,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,11,4,7,11,2,4,2,0,4,-1,-1,-1,-1,-1,-1,9,0,1,8,4,7,2,3,11,-1,-1,-1,-1,-1,-1,4,7,11,9,4,11,9,11,2,9,2,1,-1,-1,-1,3,10,1,3,11,10,7,8,4,-1,-1,-1,-1,-1,-1,1,11,10,1,4,11,1,0,4,7,11,4,-1,-1,-1,4,7,8,9,0,11,9,11,10,11,0,3,-1,-1,-1,4,7,11,4,11,9,9,11,10,-1,-1,-1,-1,-1,-1,9,5,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,5,4,0,8,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,5,4,1,5,0,-1,-1,-1,-1,-1,-1,-1,-1,-1,8,5,4,8,3,5,3,1,5,-1,-1,-1,-1,-1,-1,1,2,10,9,5,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,0,8,1,2,10,4,9,5,-1,-1,-1,-1,-1,-1,5,2,10,5,4,2,4,0,2,-1,-1,-1,-1,-1,-1,2,10,5,3,2,5,3,5,4,3,4,8,-1,-1,-1,9,5,4,2,3,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,11,2,0,8,11,4,9,5,-1,-1,-1,-1,-1,-1,0,5,4,0,1,5,2,3,11,-1,-1,-1,-1,-1,-1,2,1,5,2,5,8,2,8,11,4,8,5,-1,-1,-1,10,3,11,10,1,3,9,5,4,-1,-1,-1,-1,-1,-1,4,9,5,0,8,1,8,10,1,8,11,10,-1,-1,-1,5,4,0,5,0,11,5,11,10,11,0,3,-1,-1,-1,5,4,8,5,8,10,10,8,11,-1,-1,-1,-1,-1,-1,9,7,8,5,7,9,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,3,0,9,5,3,5,7,3,-1,-1,-1,-1,-1,-1,0,7,8,0,1,7,1,5,7,-1,-1,-1,-1,-1,-1,1,5,3,3,5,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,7,8,9,5,7,10,1,2,-1,-1,-1,-1,-1,-1,10,1,2,9,5,0,5,3,0,5,7,3,-1,-1,-1,8,0,2,8,2,5,8,5,7,10,5,2,-1,-1,-1,2,10,5,2,5,3,3,5,7,-1,-1,-1,-1,-1,-1,7,9,5,7,8,9,3,11,2,-1,-1,-1,-1,-1,-1,9,5,7,9,7,2,9,2,0,2,7,11,-1,-1,-1,2,3,11,0,1,8,1,7,8,1,5,7,-1,-1,-1,11,2,1,11,1,7,7,1,5,-1,-1,-1,-1,-1,-1,9,5,8,8,5,7,10,1,3,10,3,11,-1,-1,-1,5,7,0,5,0,9,7,11,0,1,0,10,11,10,0,11,10,0,11,0,3,10,5,0,8,0,7,5,7,0,11,10,5,7,11,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,10,6,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,8,3,5,10,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,0,1,5,10,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,8,3,1,9,8,5,10,6,-1,-1,-1,-1,-1,-1,1,6,5,2,6,1,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,6,5,1,2,6,3,0,8,-1,-1,-1,-1,-1,-1,9,6,5,9,0,6,0,2,6,-1,-1,-1,-1,-1,-1,5,9,8,5,8,2,5,2,6,3,2,8,-1,-1,-1,2,3,11,10,6,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,11,0,8,11,2,0,10,6,5,-1,-1,-1,-1,-1,-1,0,1,9,2,3,11,5,10,6,-1,-1,-1,-1,-1,-1,5,10,6,1,9,2,9,11,2,9,8,11,-1,-1,-1,6,3,11,6,5,3,5,1,3,-1,-1,-1,-1,-1,-1,0,8,11,0,11,5,0,5,1,5,11,6,-1,-1,-1,3,11,6,0,3,6,0,6,5,0,5,9,-1,-1,-1,6,5,9,6,9,11,11,9,8,-1,-1,-1,-1,-1,-1,5,10,6,4,7,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,3,0,4,7,3,6,5,10,-1,-1,-1,-1,-1,-1,1,9,0,5,10,6,8,4,7,-1,-1,-1,-1,-1,-1,10,6,5,1,9,7,1,7,3,7,9,4,-1,-1,-1,6,1,2,6,5,1,4,7,8,-1,-1,-1,-1,-1,-1,1,2,5,5,2,6,3,0,4,3,4,7,-1,-1,-1,8,4,7,9,0,5,0,6,5,0,2,6,-1,-1,-1,7,3,9,7,9,4,3,2,9,5,9,6,2,6,9,3,11,2,7,8,4,10,6,5,-1,-1,-1,-1,-1,-1,5,10,6,4,7,2,4,2,0,2,7,11,-1,-1,-1,0,1,9,4,7,8,2,3,11,5,10,6,-1,-1,-1,9,2,1,9,11,2,9,4,11,7,11,4,5,10,6,8,4,7,3,11,5,3,5,1,5,11,6,-1,-1,-1,5,1,11,5,11,6,1,0,11,7,11,4,0,4,11,0,5,9,0,6,5,0,3,6,11,6,3,8,4,7,6,5,9,6,9,11,4,7,9,7,11,9,-1,-1,-1,10,4,9,6,4,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,10,6,4,9,10,0,8,3,-1,-1,-1,-1,-1,-1,10,0,1,10,6,0,6,4,0,-1,-1,-1,-1,-1,-1,8,3,1,8,1,6,8,6,4,6,1,10,-1,-1,-1,1,4,9,1,2,4,2,6,4,-1,-1,-1,-1,-1,-1,3,0,8,1,2,9,2,4,9,2,6,4,-1,-1,-1,0,2,4,4,2,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,8,3,2,8,2,4,4,2,6,-1,-1,-1,-1,-1,-1,10,4,9,10,6,4,11,2,3,-1,-1,-1,-1,-1,-1,0,8,2,2,8,11,4,9,10,4,10,6,-1,-1,-1,3,11,2,0,1,6,0,6,4,6,1,10,-1,-1,-1,6,4,1,6,1,10,4,8,1,2,1,11,8,11,1,9,6,4,9,3,6,9,1,3,11,6,3,-1,-1,-1,8,11,1,8,1,0,11,6,1,9,1,4,6,4,1,3,11,6,3,6,0,0,6,4,-1,-1,-1,-1,-1,-1,6,4,8,11,6,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,7,10,6,7,8,10,8,9,10,-1,-1,-1,-1,-1,-1,0,7,3,0,10,7,0,9,10,6,7,10,-1,-1,-1,10,6,7,1,10,7,1,7,8,1,8,0,-1,-1,-1,10,6,7,10,7,1,1,7,3,-1,-1,-1,-1,-1,-1,1,2,6,1,6,8,1,8,9,8,6,7,-1,-1,-1,2,6,9,2,9,1,6,7,9,0,9,3,7,3,9,7,8,0,7,0,6,6,0,2,-1,-1,-1,-1,-1,-1,7,3,2,6,7,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,2,3,11,10,6,8,10,8,9,8,6,7,-1,-1,-1,2,0,7,2,7,11,0,9,7,6,7,10,9,10,7,1,8,0,1,7,8,1,10,7,6,7,10,2,3,11,11,2,1,11,1,7,10,6,1,6,7,1,-1,-1,-1,8,9,6,8,6,7,9,1,6,11,6,3,1,3,6,0,9,1,11,6,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,7,8,0,7,0,6,3,11,0,11,6,0,-1,-1,-1,7,11,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,7,6,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,0,8,11,7,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,1,9,11,7,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,8,1,9,8,3,1,11,7,6,-1,-1,-1,-1,-1,-1,10,1,2,6,11,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,2,10,3,0,8,6,11,7,-1,-1,-1,-1,-1,-1,2,9,0,2,10,9,6,11,7,-1,-1,-1,-1,-1,-1,6,11,7,2,10,3,10,8,3,10,9,8,-1,-1,-1,7,2,3,6,2,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,7,0,8,7,6,0,6,2,0,-1,-1,-1,-1,-1,-1,2,7,6,2,3,7,0,1,9,-1,-1,-1,-1,-1,-1,1,6,2,1,8,6,1,9,8,8,7,6,-1,-1,-1,10,7,6,10,1,7,1,3,7,-1,-1,-1,-1,-1,-1,10,7,6,1,7,10,1,8,7,1,0,8,-1,-1,-1,0,3,7,0,7,10,0,10,9,6,10,7,-1,-1,-1,7,6,10,7,10,8,8,10,9,-1,-1,-1,-1,-1,-1,6,8,4,11,8,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,6,11,3,0,6,0,4,6,-1,-1,-1,-1,-1,-1,8,6,11,8,4,6,9,0,1,-1,-1,-1,-1,-1,-1,9,4,6,9,6,3,9,3,1,11,3,6,-1,-1,-1,6,8,4,6,11,8,2,10,1,-1,-1,-1,-1,-1,-1,1,2,10,3,0,11,0,6,11,0,4,6,-1,-1,-1,4,11,8,4,6,11,0,2,9,2,10,9,-1,-1,-1,10,9,3,10,3,2,9,4,3,11,3,6,4,6,3,8,2,3,8,4,2,4,6,2,-1,-1,-1,-1,-1,-1,0,4,2,4,6,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,9,0,2,3,4,2,4,6,4,3,8,-1,-1,-1,1,9,4,1,4,2,2,4,6,-1,-1,-1,-1,-1,-1,8,1,3,8,6,1,8,4,6,6,10,1,-1,-1,-1,10,1,0,10,0,6,6,0,4,-1,-1,-1,-1,-1,-1,4,6,3,4,3,8,6,10,3,0,3,9,10,9,3,10,9,4,6,10,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,9,5,7,6,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,8,3,4,9,5,11,7,6,-1,-1,-1,-1,-1,-1,5,0,1,5,4,0,7,6,11,-1,-1,-1,-1,-1,-1,11,7,6,8,3,4,3,5,4,3,1,5,-1,-1,-1,9,5,4,10,1,2,7,6,11,-1,-1,-1,-1,-1,-1,6,11,7,1,2,10,0,8,3,4,9,5,-1,-1,-1,7,6,11,5,4,10,4,2,10,4,0,2,-1,-1,-1,3,4,8,3,5,4,3,2,5,10,5,2,11,7,6,7,2,3,7,6,2,5,4,9,-1,-1,-1,-1,-1,-1,9,5,4,0,8,6,0,6,2,6,8,7,-1,-1,-1,3,6,2,3,7,6,1,5,0,5,4,0,-1,-1,-1,6,2,8,6,8,7,2,1,8,4,8,5,1,5,8,9,5,4,10,1,6,1,7,6,1,3,7,-1,-1,-1,1,6,10,1,7,6,1,0,7,8,7,0,9,5,4,4,0,10,4,10,5,0,3,10,6,10,7,3,7,10,7,6,10,7,10,8,5,4,10,4,8,10,-1,-1,-1,6,9,5,6,11,9,11,8,9,-1,-1,-1,-1,-1,-1,3,6,11,0,6,3,0,5,6,0,9,5,-1,-1,-1,0,11,8,0,5,11,0,1,5,5,6,11,-1,-1,-1,6,11,3,6,3,5,5,3,1,-1,-1,-1,-1,-1,-1,1,2,10,9,5,11,9,11,8,11,5,6,-1,-1,-1,0,11,3,0,6,11,0,9,6,5,6,9,1,2,10,11,8,5,11,5,6,8,0,5,10,5,2,0,2,5,6,11,3,6,3,5,2,10,3,10,5,3,-1,-1,-1,5,8,9,5,2,8,5,6,2,3,8,2,-1,-1,-1,9,5,6,9,6,0,0,6,2,-1,-1,-1,-1,-1,-1,1,5,8,1,8,0,5,6,8,3,8,2,6,2,8,1,5,6,2,1,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,3,6,1,6,10,3,8,6,5,6,9,8,9,6,10,1,0,10,0,6,9,5,0,5,6,0,-1,-1,-1,0,3,8,5,6,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,10,5,6,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,11,5,10,7,5,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,11,5,10,11,7,5,8,3,0,-1,-1,-1,-1,-1,-1,5,11,7,5,10,11,1,9,0,-1,-1,-1,-1,-1,-1,10,7,5,10,11,7,9,8,1,8,3,1,-1,-1,-1,11,1,2,11,7,1,7,5,1,-1,-1,-1,-1,-1,-1,0,8,3,1,2,7,1,7,5,7,2,11,-1,-1,-1,9,7,5,9,2,7,9,0,2,2,11,7,-1,-1,-1,7,5,2,7,2,11,5,9,2,3,2,8,9,8,2,2,5,10,2,3,5,3,7,5,-1,-1,-1,-1,-1,-1,8,2,0,8,5,2,8,7,5,10,2,5,-1,-1,-1,9,0,1,5,10,3,5,3,7,3,10,2,-1,-1,-1,9,8,2,9,2,1,8,7,2,10,2,5,7,5,2,1,3,5,3,7,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,8,7,0,7,1,1,7,5,-1,-1,-1,-1,-1,-1,9,0,3,9,3,5,5,3,7,-1,-1,-1,-1,-1,-1,9,8,7,5,9,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,5,8,4,5,10,8,10,11,8,-1,-1,-1,-1,-1,-1,5,0,4,5,11,0,5,10,11,11,3,0,-1,-1,-1,0,1,9,8,4,10,8,10,11,10,4,5,-1,-1,-1,10,11,4,10,4,5,11,3,4,9,4,1,3,1,4,2,5,1,2,8,5,2,11,8,4,5,8,-1,-1,-1,0,4,11,0,11,3,4,5,11,2,11,1,5,1,11,0,2,5,0,5,9,2,11,5,4,5,8,11,8,5,9,4,5,2,11,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,2,5,10,3,5,2,3,4,5,3,8,4,-1,-1,-1,5,10,2,5,2,4,4,2,0,-1,-1,-1,-1,-1,-1,3,10,2,3,5,10,3,8,5,4,5,8,0,1,9,5,10,2,5,2,4,1,9,2,9,4,2,-1,-1,-1,8,4,5,8,5,3,3,5,1,-1,-1,-1,-1,-1,-1,0,4,5,1,0,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,8,4,5,8,5,3,9,0,5,0,3,5,-1,-1,-1,9,4,5,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,11,7,4,9,11,9,10,11,-1,-1,-1,-1,-1,-1,0,8,3,4,9,7,9,11,7,9,10,11,-1,-1,-1,1,10,11,1,11,4,1,4,0,7,4,11,-1,-1,-1,3,1,4,3,4,8,1,10,4,7,4,11,10,11,4,4,11,7,9,11,4,9,2,11,9,1,2,-1,-1,-1,9,7,4,9,11,7,9,1,11,2,11,1,0,8,3,11,7,4,11,4,2,2,4,0,-1,-1,-1,-1,-1,-1,11,7,4,11,4,2,8,3,4,3,2,4,-1,-1,-1,2,9,10,2,7,9,2,3,7,7,4,9,-1,-1,-1,9,10,7,9,7,4,10,2,7,8,7,0,2,0,7,3,7,10,3,10,2,7,4,10,1,10,0,4,0,10,1,10,2,8,7,4,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,9,1,4,1,7,7,1,3,-1,-1,-1,-1,-1,-1,4,9,1,4,1,7,0,8,1,8,7,1,-1,-1,-1,4,0,3,7,4,3,-1,-1,-1,-1,-1,-1,-1,-1,-1,4,8,7,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,9,10,8,10,11,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,0,9,3,9,11,11,9,10,-1,-1,-1,-1,-1,-1,0,1,10,0,10,8,8,10,11,-1,-1,-1,-1,-1,-1,3,1,10,11,3,10,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,2,11,1,11,9,9,11,8,-1,-1,-1,-1,-1,-1,3,0,9,3,9,11,1,2,9,2,11,9,-1,-1,-1,0,2,11,8,0,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,3,2,11,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,2,3,8,2,8,10,10,8,9,-1,-1,-1,-1,-1,-1,9,10,2,0,9,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,2,3,8,2,8,10,0,1,8,1,10,8,-1,-1,-1,1,10,2,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,1,3,8,9,1,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,9,1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,0,3,8,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1,-1}

    var edgeListABO uint32 = 0
    gl.GenBuffers    (1, &edgeListABO);
    gl.BindBuffer    (gl.ARRAY_BUFFER, edgeListABO);
    gl.BufferData    (gl.ARRAY_BUFFER, 256*5*3*int(unsafe.Sizeof(int32(0))), gl.Ptr(edgeConnectList), gl.STATIC_READ);

    gl.UseProgram(marchingCubesShaderID)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 1, edgeListABO);
    gl.UseProgram(0)


}

// Here, the actual triangles are calculated and written into by the second marching cube shader invocation.
// This buffer is later used for rendering!
// Returns the positionArrayBuffer and vertexArrayBuffer.
func createPositionBuffers(totalTriangleCount int) (uint32, uint32) {

    var positionArrayBuffer  uint32
    var positionVertexBuffer uint32

    emptyVec := mgl32.Vec4{}
    vec4Size := int(unsafe.Sizeof(emptyVec))

    emptyVertex := Vertex{}
    stride := int(unsafe.Sizeof(emptyVertex))

    triangleSize := 3*stride

    computeTriangles := make([]Triangle, totalTriangleCount)
    for i,_ := range computeTriangles  {
        computeTriangles[i] = Triangle{make([]Vertex, 3)}
    }

    gl.GenBuffers    (1, &positionArrayBuffer);
    gl.BindBuffer    (gl.ARRAY_BUFFER, positionArrayBuffer);
    // Data is ordered linear in memory :) (https://research.swtch.com/godata)
    gl.BufferData    (gl.ARRAY_BUFFER, totalTriangleCount * triangleSize, gl.Ptr(&computeTriangles[0].Vertices[0].Pos[0]), gl.DYNAMIC_DRAW);

    gl.GenVertexArrays(1, &positionVertexBuffer)
    gl.BindVertexArray(positionVertexBuffer)

    gl.EnableVertexAttribArray(0)
    gl.VertexAttribPointer(0, 4, gl.FLOAT, false, int32(stride), gl.PtrOffset(0))
    gl.EnableVertexAttribArray(1)
    // If adding more attributes to a vertex, change the Offset and potentially stride here!
    gl.VertexAttribPointer(1, 4, gl.FLOAT, true, int32(stride), gl.PtrOffset(vec4Size))

    return positionArrayBuffer, positionVertexBuffer
}

// Each small cube writes into this buffer, how many triangles it wants to create.
// Using this information, we can later fill the position buffer up, without having
// empty positions.
func createTriangleLayoutSizeBuffer(totalCubeCount int, marchingCubesShaderID uint32) uint32 {

    triangleLayoutSizes := make([]int32, totalCubeCount)
    var triangleLayoutSizesBuffer uint32

    gl.GenBuffers    (1, &triangleLayoutSizesBuffer);
    gl.BindBuffer    (gl.ARRAY_BUFFER, triangleLayoutSizesBuffer);
    gl.BufferData    (gl.ARRAY_BUFFER, totalCubeCount*int(unsafe.Sizeof(int32(0))), gl.Ptr(&triangleLayoutSizes[0]), gl.DYNAMIC_COPY);

    gl.UseProgram(marchingCubesShaderID)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 3, triangleLayoutSizesBuffer);
    gl.UseProgram(0)

    return triangleLayoutSizesBuffer

}


// A Buffer where the actual cases (for all corners of the cube) are written into.
func createCasesBuffer(totalCubeCount int, marchingCubesShaderID uint32) {
    cases := make([]int32, totalCubeCount)

    var casesABO uint32 = 0
    gl.GenBuffers    (1, &casesABO);
    gl.BindBuffer    (gl.ARRAY_BUFFER, casesABO);
    gl.BufferData    (gl.ARRAY_BUFFER, totalCubeCount*int(unsafe.Sizeof(int32(0))), gl.Ptr(&cases[0]), gl.STATIC_READ);

    gl.UseProgram(marchingCubesShaderID)
    gl.BindBufferBase(gl.SHADER_STORAGE_BUFFER, 4, casesABO);
    gl.UseProgram(0)
}

func main() {
    var err error = nil
    if err = glfw.Init(); err != nil {
        panic(err)
    }
    // Terminate as soon, as this the function is finished.
    defer glfw.Terminate()

    window, err := initGraphicContext()
    if err != nil {
        // Decision to panic or do something different is taken in the main
        // method and not in sub-functions
        panic(err)
    }

    path := "../Go/src/GPUTerrain/"
    g_ShaderID, err = NewProgram(path+"vertexShader.vert", path+"fragmentShader.frag")
    if err != nil {
        panic(err)
    }



    g_light = CreateObject(CreateUnitSphere(10), mgl32.Vec3{3,15,0}, mgl32.Vec3{0.2,0.2,0.2}, mgl32.Vec3{1,1,0}, true)


    trianglesPerCube        := float32(2.0)
    marchingCubeCountWidth  := 15
    marchingCubeCountHeight := 1
    marchingCubeCountDepth  := 15
    marchingCubeCount := marchingCubeCountWidth * marchingCubeCountHeight * marchingCubeCountDepth
    marchingCubeUnits := make([]MarchingCubeUnit, marchingCubeCount, marchingCubeCount)

    addedLocalWorkgroupCount := 0

    for x := 0; x < marchingCubeCountWidth; x+=1 {
        for y := 0; y < marchingCubeCountHeight; y+=1 {
            for z := 0; z < marchingCubeCountDepth; z+=1 {

                i := int(z * marchingCubeCountWidth * marchingCubeCountHeight + y * marchingCubeCountWidth + x)

                marchingCubeUnits[i] = MarchingCubeUnit {
                    PositionOffset:         mgl32.Vec3{float32(x*g_cubeWidth),float32(y*g_cubeHeight),float32(z*g_cubeDepth)},
                    LocalWorkGroupCount:    g_cubeWidth * g_cubeHeight * g_cubeDepth,
                    RenderTriangleCount:    0,
                    BoxOutline:             CreateObject(CreateUnitCube(1), mgl32.Vec3{float32(x*g_cubeWidth+5),float32(y*g_cubeHeight+5),float32(z*g_cubeDepth+5)}, mgl32.Vec3{10,10,10}, mgl32.Vec3{1,0,0}, false),
                    ShowOutline:            true,
                }
                addedLocalWorkgroupCount += marchingCubeUnits[i].LocalWorkGroupCount
            }
        }
    }

    marchingCubesProgram, err := NewComputeProgram(path+"marchingCubes.comp")
    if err != nil {
        panic(err)
    }

    positionArrayBuffer, positionVertexBuffer := createPositionBuffers(int(float32(addedLocalWorkgroupCount) * trianglesPerCube))
    triangleLayoutSizesBuffer := createTriangleLayoutSizeBuffer(addedLocalWorkgroupCount, marchingCubesProgram)
    createMarchingCubeConstBuffers(marchingCubesProgram)
    createCasesBuffer(addedLocalWorkgroupCount, marchingCubesProgram)

    g_marchingCubes = MarchingCubes {
        ShaderID:               marchingCubesProgram,
        // Absolute worst-case!!! Correct that later.
        TriangleCount:          int(float32(addedLocalWorkgroupCount) * trianglesPerCube),

        PositionArrayBuffer:    positionArrayBuffer,
        PositionVertexBuffer:   positionVertexBuffer,
        TriangleLayoutSizesBuffer: triangleLayoutSizesBuffer,


        UnitCount:              marchingCubeCount,
        MarchingCubeUnits:      marchingCubeUnits,
    }

    //g_marchingCubesBoxOutline = CreateObject(CreateUnitCube(1), mgl32.Vec3{5,5,5}, mgl32.Vec3{10,10,10}, mgl32.Vec3{1,0,0}, false)






    gl.PointSize(3.0);


    mainLoop(window)

}




