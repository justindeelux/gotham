import{_ as ve,dQ as ge,dR as me,a0 as Y,aS as a,cs as X,$ as N,as as s,ct as J,d as we,a1 as G,dS as U,o as f,c as y,a as k,a3 as l,al as i,ak as Q,a4 as pe,cJ as xe,p as H,au as ye,g as K,aY as O,aA as p,x as q,dj as ke,a2 as Se,cx as Ce,a6 as x,a7 as E,dh as d,a$ as Be}from"./index-CcWWAHUE.js";import{u as _e}from"./use-merged-state-uLIHPHJR.js";function Re(e){const{primaryColor:c,opacityDisabled:b,borderRadius:n,textColor3:S}=e;return{...ge,iconColor:S,textColor:"white",loadingColor:c,opacityDisabled:b,railColor:"rgba(0, 0, 0, .14)",railColorActive:c,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:n,railBorderRadiusMedium:n,railBorderRadiusLarge:n,buttonBorderRadiusSmall:n,buttonBorderRadiusMedium:n,buttonBorderRadiusLarge:n,boxShadowFocus:`0 0 0 2px ${me(c,{alpha:.2})}`}}const $e={common:ve,self:Re};var ze=Y("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[a("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),a("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),a("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),Y("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[X({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),a("checked, unchecked",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),a("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),a("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),N("&:focus",[a("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),s("round",[a("rail","border-radius: calc(var(--n-rail-height) / 2);",[a("button","border-radius: calc(var(--n-button-height) / 2);")])]),J("disabled",[J("icon",[s("rubber-band",[s("pressed",[a("rail",[a("button","max-width: var(--n-button-width-pressed);")])]),a("rail",[N("&:active",[a("button","max-width: var(--n-button-width-pressed);")])]),s("active",[s("pressed",[a("rail",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),a("rail",[N("&:active",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),s("active",[a("rail",[a("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),a("rail",`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[a("button-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[X()]),a("button",`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),s("active",[a("rail","background-color: var(--n-rail-color-active);")]),s("loading",[a("rail",`
 cursor: wait;
 `)]),s("disabled",[a("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]);const Ve=["aria-checked","tabindex","onClick","onFocus","onBlur","onKeyup","onKeydown"],Fe={...G.props,size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]};let $;var Ke=we({name:"Switch",props:Fe,slots:Object,setup(e){$===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?$=CSS.supports("width","max(1px)"):$=!1:$=!0);const{mergedClsPrefixRef:c,inlineThemeDisabled:b,mergedComponentPropsRef:n}=pe(e),S=G("Switch","-switch",ze,$e,e,c),v=xe(e,{mergedSize(t){if(e.size!==void 0)return e.size;if(t)return t.mergedSize.value;const w=n?.value?.Switch?.size;return w||"medium"}}),{mergedSizeRef:C,mergedDisabledRef:g}=v,B=H(e.defaultValue),z=Be(e,"value"),m=_e(z,B),V=K(()=>m.value===e.checkedValue),o=H(!1),r=H(!1),_=K(()=>{const{railStyle:t}=e;if(t)return t({focused:r.value,checked:V.value})});function F(t){const{"onUpdate:value":w,onChange:P,onUpdateValue:T}=e,{nTriggerFormInput:D,nTriggerFormChange:W}=v;w&&O(w,t),T&&O(T,t),P&&O(P,t),B.value=t,D(),W()}function Z(){const{nTriggerFormFocus:t}=v;t()}function ee(){const{nTriggerFormBlur:t}=v;t()}function te(){e.loading||g.value||(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue))}function ae(){r.value=!0,Z()}function oe(){r.value=!1,ee(),o.value=!1}function ie(t){e.loading||g.value||t.key===" "&&(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue),o.value=!1)}function ne(t){e.loading||g.value||t.key===" "&&(t.preventDefault(),o.value=!0)}const L=K(()=>{const{value:t}=C,{self:{opacityDisabled:w,railColor:P,railColorActive:T,buttonBoxShadow:D,buttonColor:W,boxShadowFocus:re,loadingColor:le,textColor:se,iconColor:de,[x("buttonHeight",t)]:u,[x("buttonWidth",t)]:ce,[x("buttonWidthPressed",t)]:ue,[x("railHeight",t)]:h,[x("railWidth",t)]:R,[x("railBorderRadius",t)]:he,[x("buttonBorderRadius",t)]:fe},common:{cubicBezierEaseInOut:be}}=S.value;let M,j,A;return $?(M=`calc((${h} - ${u}) / 2)`,j=`max(${h}, ${u})`,A=`max(${R}, calc(${R} + ${u} - ${h}))`):(M=E((d(h)-d(u))/2),j=E(Math.max(d(h),d(u))),A=d(h)>d(u)?R:E(d(R)+d(u)-d(h))),{"--n-bezier":be,"--n-button-border-radius":fe,"--n-button-box-shadow":D,"--n-button-color":W,"--n-button-width":ce,"--n-button-width-pressed":ue,"--n-button-height":u,"--n-height":j,"--n-offset":M,"--n-opacity-disabled":w,"--n-rail-border-radius":he,"--n-rail-color":P,"--n-rail-color-active":T,"--n-rail-height":h,"--n-rail-width":R,"--n-width":A,"--n-box-shadow-focus":re,"--n-loading-color":le,"--n-text-color":se,"--n-icon-color":de}}),I=b?ye("switch",K(()=>C.value[0]),L,e):void 0;return{handleClick:te,handleBlur:oe,handleFocus:ae,handleKeyup:ie,handleKeydown:ne,mergedRailStyle:_,pressed:o,mergedClsPrefix:c,mergedValue:m,checked:V,mergedDisabled:g,cssVars:b?void 0:L,themeClass:I?.themeClass,onRender:I?.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:c,checked:b,mergedRailStyle:n,onRender:S,$slots:v}=this;S?.();const{checked:C,unchecked:g,icon:B,"checked-icon":z,"unchecked-icon":m}=v,V=!(U(B)&&U(z)&&U(m));return f(),y("div",{role:"switch","aria-checked":b,class:i([`${e}-switch`,this.themeClass,V&&`${e}-switch--icon`,b&&`${e}-switch--active`,c&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`]),tabindex:this.mergedDisabled?void 0:0,style:Q(this.cssVars),onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},[k("div",{class:i(`${e}-switch__rail`),"aria-hidden":"true",style:Q(n)},[l(()=>p(C,o=>p(g,r=>o||r?(f(),y("div",{key:4,"aria-hidden":!0,class:i(`${e}-switch__children-placeholder`)},[k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>o)],2),k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>r)],2)],2)):null))),k("div",{class:i(`${e}-switch__button`)},[l(()=>p(B,o=>p(z,r=>p(m,_=>(f(),q(Ce,null,{default:()=>this.loading?(f(),q(ke,Se({key:"loading",clsPrefix:e,strokeWidth:20},this.spinProps),null,16,["clsPrefix"])):this.checked&&(r||o)?(f(),y("div",{class:i(`${e}-switch__button-icon`),key:r?"checked-icon":"icon"},[l(()=>r||o)],2)):!this.checked&&(_||o)?(f(),y("div",{class:i(`${e}-switch__button-icon`),key:_?"unchecked-icon":"icon"},[l(()=>_||o)],2)):null},1024)))))),l(()=>p(C,o=>o&&(f(),y("div",{key:"checked",class:i(`${e}-switch__checked`)},[l(()=>o)],2)))),l(()=>p(g,o=>o&&(f(),y("div",{key:"unchecked",class:i(`${e}-switch__unchecked`)},[l(()=>o)],2))))],2)],6)],46,Ve)}});export{Ke as S};
